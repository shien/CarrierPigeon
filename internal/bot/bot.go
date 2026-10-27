// Package bot owns the Discord gateway session and the slash commands
// that manage subscriptions. Everything is outbound: the gateway is a
// WebSocket we dial out, so no inbound connectivity is ever required.
package bot

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/shien/carrierpigeon/internal/store"
	"github.com/shien/carrierpigeon/internal/watcher/youtube"
)

// ChannelResolver turns user input into a canonical channel ID + title.
type ChannelResolver interface {
	ResolveChannel(ctx context.Context, input string) (id, title string, err error)
}

// Seeder marks a channel's existing videos as notified (watcher.Seed).
type Seeder interface {
	Seed(ctx context.Context, channelID string) error
}

type Bot struct {
	Session  *discordgo.Session
	Store    *store.Store
	Resolver ChannelResolver
	Seeder   Seeder
	Log      *slog.Logger
}

func New(token string, st *store.Store, resolver ChannelResolver, seeder Seeder, log *slog.Logger) (*Bot, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("create discord session: %w", err)
	}
	session.Identify.Intents = discordgo.IntentsGuilds

	b := &Bot{Session: session, Store: st, Resolver: resolver, Seeder: seeder, Log: log}
	session.AddHandler(b.onReady)
	session.AddHandler(b.onGuildCreate)
	session.AddHandler(b.onInteraction)
	return b, nil
}

func (b *Bot) Open() error  { return b.Session.Open() }
func (b *Bot) Close() error { return b.Session.Close() }

var manageGuild int64 = discordgo.PermissionManageGuild

var commands = []*discordgo.ApplicationCommand{
	{
		Name:                     "subscribe",
		Description:              "YouTube チャンネルの新着・配信通知を登録します",
		DefaultMemberPermissions: &manageGuild,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "channel",
				Description: "チャンネル URL・UC から始まる ID・@ハンドルのいずれか",
				Required:    true,
			},
			{
				Type:         discordgo.ApplicationCommandOptionChannel,
				Name:         "notify_channel",
				Description:  "通知先チャンネル(省略時はこのチャンネル)",
				ChannelTypes: []discordgo.ChannelType{discordgo.ChannelTypeGuildText, discordgo.ChannelTypeGuildNews},
			},
		},
	},
	{
		Name:                     "unsubscribe",
		Description:              "YouTube チャンネルの通知登録を解除します",
		DefaultMemberPermissions: &manageGuild,
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "channel",
				Description: "チャンネル URL・UC から始まる ID・@ハンドルのいずれか",
				Required:    true,
			},
		},
	},
	{
		Name:        "list",
		Description: "このサーバーで監視中の YouTube チャンネルを表示します",
	},
}

func (b *Bot) onReady(s *discordgo.Session, r *discordgo.Ready) {
	b.Log.Info("discord gateway ready", "user", r.User.Username, "guilds", len(r.Guilds))
}

// Registering per guild (instead of globally) makes command changes
// visible immediately instead of after up to an hour of propagation.
func (b *Bot) onGuildCreate(s *discordgo.Session, g *discordgo.GuildCreate) {
	if _, err := s.ApplicationCommandBulkOverwrite(s.State.User.ID, g.ID, commands); err != nil {
		b.Log.Error("register commands failed", "guild", g.ID, "error", err)
		return
	}
	b.Log.Info("registered commands", "guild", g.ID)
}

func (b *Bot) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if i.Type != discordgo.InteractionApplicationCommand {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Interactions must be acknowledged within 3 seconds, but subscribe
	// resolves the channel and seeds its backlog over the network — so
	// always defer first, then edit the response with the result.
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	})
	if err != nil {
		b.Log.Error("interaction ack failed", "error", err)
		return
	}

	var reply string
	switch i.ApplicationCommandData().Name {
	case "subscribe":
		reply, err = b.handleSubscribe(ctx, i)
	case "unsubscribe":
		reply, err = b.handleUnsubscribe(ctx, i)
	case "list":
		reply, err = b.handleList(ctx, i)
	default:
		return
	}
	if err != nil {
		b.Log.Error("command failed", "command", i.ApplicationCommandData().Name, "error", err)
		reply = "エラーが発生しました: " + err.Error()
	}
	if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &reply}); err != nil {
		b.Log.Error("interaction respond failed", "error", err)
	}
}

func (b *Bot) handleSubscribe(ctx context.Context, i *discordgo.InteractionCreate) (string, error) {
	opts := optionMap(i)
	input := opts["channel"].StringValue()

	notifyChannelID := i.ChannelID
	if opt, ok := opts["notify_channel"]; ok {
		notifyChannelID = opt.Value.(string)
		// The option's UI is restricted to channels in this guild, but
		// that's a client-side convenience only: a forged interaction
		// could name a channel in a different guild entirely. Confirm
		// server-side before letting it become a notification target.
		if err := b.requireChannelInGuild(notifyChannelID, i.GuildID); err != nil {
			return "", err
		}
	}

	id, title, err := b.Resolver.ResolveChannel(ctx, input)
	if err != nil {
		return "", fmt.Errorf("チャンネルを特定できませんでした (%w)", err)
	}

	// Seed BEFORE the subscription becomes visible to the poller, so a
	// poll cycle can never race in and announce the channel's backlog.
	known, err := b.Store.HasChannel(ctx, id)
	if err != nil {
		return "", err
	}
	if !known {
		if err := b.Seeder.Seed(ctx, id); err != nil {
			return "", fmt.Errorf("既存動画の初期化に失敗しました (%w)", err)
		}
	}

	err = b.Store.AddSubscription(ctx, store.Subscription{
		YTChannelID:      id,
		YTChannelName:    title,
		DiscordGuildID:   i.GuildID,
		DiscordChannelID: notifyChannelID,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("✅ **%s** (`%s`) を登録しました。通知先: <#%s>", title, id, notifyChannelID), nil
}

func (b *Bot) handleUnsubscribe(ctx context.Context, i *discordgo.InteractionCreate) (string, error) {
	input := optionMap(i)["channel"].StringValue()

	// Try to avoid an API call: a UC... ID or URL containing one parses
	// directly; otherwise resolve the handle through the API.
	id := input
	if ref, err := youtube.ParseChannelInput(input); err == nil && ref.Kind == youtube.ChannelRefID {
		id = ref.Value
	} else {
		resolved, _, err := b.Resolver.ResolveChannel(ctx, input)
		if err != nil {
			return "", fmt.Errorf("チャンネルを特定できませんでした (%w)", err)
		}
		id = resolved
	}

	removed, err := b.Store.RemoveSubscription(ctx, i.GuildID, id)
	if err != nil {
		return "", err
	}
	if !removed {
		return fmt.Sprintf("`%s` はこのサーバーでは登録されていません。`/list` で確認できます。", id), nil
	}
	return fmt.Sprintf("🗑️ `%s` の登録を解除しました。", id), nil
}

func (b *Bot) handleList(ctx context.Context, i *discordgo.InteractionCreate) (string, error) {
	subs, err := b.Store.ListSubscriptionsByGuild(ctx, i.GuildID)
	if err != nil {
		return "", err
	}
	if len(subs) == 0 {
		return "監視中のチャンネルはありません。`/subscribe` で追加できます。", nil
	}
	var sb strings.Builder
	sb.WriteString("**監視中のチャンネル**\n")
	for _, s := range subs {
		fmt.Fprintf(&sb, "- %s (`%s`) → <#%s>\n", s.YTChannelName, s.YTChannelID, s.DiscordChannelID)
	}
	return sb.String(), nil
}

// requireChannelInGuild confirms channelID actually belongs to guildID,
// so a tampered interaction can't point notifications at a channel in a
// different guild than the one the invoker has Manage Server on.
func (b *Bot) requireChannelInGuild(channelID, guildID string) error {
	ch, err := b.Session.State.Channel(channelID)
	if err != nil {
		ch, err = b.Session.Channel(channelID)
		if err != nil {
			return fmt.Errorf("通知先チャンネルを確認できませんでした (%w)", err)
		}
	}
	if ch.GuildID != guildID {
		return fmt.Errorf("通知先チャンネルはこのサーバーに属していません")
	}
	return nil
}

func optionMap(i *discordgo.InteractionCreate) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	m := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
	for _, opt := range i.ApplicationCommandData().Options {
		m[opt.Name] = opt
	}
	return m
}
