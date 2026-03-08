package search

import (
	"regexp"
	"strings"

	"github.com/slack-go/slack"
)

var (
	userMentionRe    = regexp.MustCompile(`<@([A-Z0-9]+)(?:\|([^>]+))?>`)
	channelMentionRe = regexp.MustCompile(`<#([A-Z0-9]+)(?:\|([^>]+))?>`)
	linkRe           = regexp.MustCompile(`<([^>|]+)\|?([^>]*)>`)
)

type Mention struct {
	Type        string
	TargetID    string
	DisplayText string
}

func NormalizeMessage(msg slack.Message) string {
	text := msg.Text
	text = userMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := userMentionRe.FindStringSubmatch(match)
		if parts[2] != "" {
			return "@" + parts[2]
		}
		return "@" + parts[1]
	})
	text = channelMentionRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := channelMentionRe.FindStringSubmatch(match)
		if parts[2] != "" {
			return "#" + parts[2]
		}
		return "#" + parts[1]
	})
	text = linkRe.ReplaceAllStringFunc(text, func(match string) string {
		parts := linkRe.FindStringSubmatch(match)
		if len(parts) < 3 {
			return match
		}
		if strings.HasPrefix(parts[1], "@") || strings.HasPrefix(parts[1], "#") {
			return match
		}
		if parts[2] != "" {
			return parts[2] + " " + parts[1]
		}
		return parts[1]
	})

	parts := []string{strings.TrimSpace(text)}
	for _, file := range msg.Files {
		if file.Title != "" {
			parts = append(parts, file.Title)
		}
		if file.Name != "" && file.Name != file.Title {
			parts = append(parts, file.Name)
		}
	}
	if msg.Edited != nil {
		parts = append(parts, "[edited]")
	}
	if msg.SubType == "message_deleted" || msg.DeletedTimestamp != "" {
		parts = append(parts, "[deleted]")
	}
	if msg.ThreadTimestamp != "" && msg.ThreadTimestamp != msg.Timestamp {
		parts = append(parts, "[thread-reply]")
	}
	return strings.TrimSpace(strings.Join(filterEmpty(parts), " "))
}

func ExtractMentions(text string) []Mention {
	var mentions []Mention
	for _, match := range userMentionRe.FindAllStringSubmatch(text, -1) {
		mentions = append(mentions, Mention{
			Type:        "user",
			TargetID:    match[1],
			DisplayText: display(match[2], match[1]),
		})
	}
	for _, match := range channelMentionRe.FindAllStringSubmatch(text, -1) {
		mentions = append(mentions, Mention{
			Type:        "channel",
			TargetID:    match[1],
			DisplayText: display(match[2], match[1]),
		})
	}
	return mentions
}

func display(label string, fallback string) string {
	if label != "" {
		return label
	}
	return fallback
}

func filterEmpty(parts []string) []string {
	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			filtered = append(filtered, strings.TrimSpace(part))
		}
	}
	return filtered
}
