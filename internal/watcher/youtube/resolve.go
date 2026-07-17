package youtube

import (
	"fmt"
	"regexp"
	"strings"
)

type ChannelRefKind int

const (
	ChannelRefID ChannelRefKind = iota
	ChannelRefHandle
	ChannelRefUsername
)

type ChannelRef struct {
	Kind  ChannelRefKind
	Value string
}

var channelIDPattern = regexp.MustCompile(`^UC[0-9A-Za-z_-]{22}$`)

// ParseChannelInput accepts the forms users are likely to paste:
//
//	UCxxxxxxxxxxxxxxxxxxxxxx           (channel ID)
//	@handle                            (handle)
//	https://www.youtube.com/channel/UC...
//	https://www.youtube.com/@handle
//	https://www.youtube.com/user/name  (legacy username)
//
// It is a pure parser; API resolution happens in ResolveChannel.
func ParseChannelInput(input string) (ChannelRef, error) {
	s := strings.TrimSpace(input)
	if channelIDPattern.MatchString(s) {
		return ChannelRef{ChannelRefID, s}, nil
	}
	if strings.HasPrefix(s, "@") {
		return ChannelRef{ChannelRefHandle, s}, nil
	}

	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	for _, host := range []string{"www.youtube.com/", "youtube.com/", "m.youtube.com/"} {
		if rest, ok := strings.CutPrefix(s, host); ok {
			rest, _, _ = strings.Cut(rest, "?")
			rest = strings.TrimSuffix(rest, "/")
			switch {
			case strings.HasPrefix(rest, "channel/"):
				id, _, _ := strings.Cut(strings.TrimPrefix(rest, "channel/"), "/")
				if channelIDPattern.MatchString(id) {
					return ChannelRef{ChannelRefID, id}, nil
				}
			case strings.HasPrefix(rest, "@"):
				if handle, _, _ := strings.Cut(rest, "/"); handle != "@" {
					return ChannelRef{ChannelRefHandle, handle}, nil
				}
			case strings.HasPrefix(rest, "user/"):
				if name := strings.TrimPrefix(rest, "user/"); name != "" {
					return ChannelRef{ChannelRefUsername, name}, nil
				}
			}
			break
		}
	}
	return ChannelRef{}, fmt.Errorf("unsupported channel reference %q: use a UC... ID, @handle, or channel URL", input)
}
