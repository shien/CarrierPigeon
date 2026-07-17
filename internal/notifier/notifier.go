// Package notifier renders events as Discord embeds and delivers them
// through the bot's gateway session.
package notifier

import (
	"context"
	"fmt"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/shien/carrierpigeon/internal/watcher/youtube"
)

const (
	colorNewVideo = 0x2eccfa // blue
	colorUpcoming = 0xf5a623 // orange
	colorLive     = 0xe74c3c // red
)

// Discord implements youtube.Notifier.
type Discord struct {
	Session *discordgo.Session
}

func (d *Discord) Notify(ctx context.Context, discordChannelID string, event youtube.Event, v youtube.Video) error {
	_, err := d.Session.ChannelMessageSendEmbed(discordChannelID, BuildEmbed(event, v), discordgo.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("send embed to %s: %w", discordChannelID, err)
	}
	return nil
}

// BuildEmbed is pure so it can be unit-tested without a session.
func BuildEmbed(event youtube.Event, v youtube.Video) *discordgo.MessageEmbed {
	embed := &discordgo.MessageEmbed{
		Title: v.Title,
		URL:   v.URL(),
		Author: &discordgo.MessageEmbedAuthor{
			Name: v.ChannelTitle,
		},
	}
	if v.Thumbnail != "" {
		embed.Image = &discordgo.MessageEmbedImage{URL: v.Thumbnail}
	}

	switch event {
	case youtube.EventUpcoming:
		embed.Color = colorUpcoming
		embed.Description = "🕒 配信予定が追加されました"
		if !v.ScheduledStart.IsZero() {
			// Discord renders <t:unix:F> in the viewer's local time zone.
			embed.Description += fmt.Sprintf("\n開始予定: <t:%d:F> (<t:%d:R>)",
				v.ScheduledStart.Unix(), v.ScheduledStart.Unix())
			embed.Timestamp = v.ScheduledStart.UTC().Format(time.RFC3339)
		}
	case youtube.EventLiveStarted:
		embed.Color = colorLive
		embed.Description = "🔴 ライブ配信が始まりました"
	default:
		embed.Color = colorNewVideo
		embed.Description = "📺 新しい動画が公開されました"
	}
	return embed
}
