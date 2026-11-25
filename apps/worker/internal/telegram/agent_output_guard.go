package telegram

import (
	"context"
	"encoding/json"
	"github.com/raufimusaddiq/richmod/apps/worker/internal/plainlabel"
	"log/slog"
	"regexp"
	"strings"
	"unicode"
)

type replyFault string

const (
	replyFaultNone      replyFault = ""
	replyFaultEmpty     replyFault = "EMPTY"
	replyFaultBroken    replyFault = "BROKEN_TEXT"
	replyFaultCode      replyFault = "INTERNAL_CODE"
	replyFaultMechanics replyFault = "TELEGRAM_MECHANICS"
)

const interactionReplyRepaired = "REPLY_REPAIRED"

const replyFallbackMessage = "Maaf, jawabanku tadi tidak rapi jadi tidak kukirim. Coba tanyakan lagi, ya."

const noFindingsMessage = "Aku belum menemukan datanya untuk itu. Coba sebutkan periode atau kata kuncinya lebih jelas, ya."

var (
	uuidPattern      = regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`)
	jsonKeyPattern   = regexp.MustCompile(`"[A-Za-z_][A-Za-z0-9_]*"\s*:\s*(?:"|\d|\{|\[|true|false|null)`)
	mechanicsPattern = regexp.MustCompile(`(?i)balas\s+(?:langsung\s+)?pesan\s+ini|balas\s+pesan\s+(?:kartu|di\s+atas|tersebut|itu)|\(\s*reply\s*\)|\btelegram\s+reply\b|reply\s+(?:to\s+)?this\s+message|reply\s+to\s+the\s+card|\bcallback\b|\bmessage[\s_-]?id\b|\bchat[\s_-]?id\b|tombol\s+di\s+atas\s+pesan|tekan\s+tombol\s+di\s+atas|swipe\s+(?:kiri|left)|(?:tap|geser)\s+(?:dan\s+)?tahan\s+pesan`)
	emptySlotPattern = regexp.MustCompile(`\w[ \t]+[.,!?](?:\s|$)|<nil>|%[sdv]\b|\{\{|\}\}|\$\{|\bundefined\b|\bnull\b|\[\s*\]|\{\s*\}`)
	loneMarkPattern  = regexp.MustCompile(`^[\s\p{P}\p{Sm}]+$`)
	htmlTagPattern   = regexp.MustCompile(`</?[a-zA-Z][a-zA-Z0-9]*[^>]*>`)
	danglingTail     = regexp.MustCompile(`(?i)(?:\b(?:dan|atau|yang|dengan|untuk|ke|di|dari|perlu|karena|seperti|and|or|the|to|of|with|for)|[,:;(\-])\s*$`)
)

func replyHasMarkupImbalance(text string) bool {
	if strings.Count(text, "```")%2 == 1 {
		return true
	}
	stripped := strings.ReplaceAll(text, "```", "")
	if strings.Count(stripped, "`")%2 == 1 || strings.Count(stripped, "**")%2 == 1 {
		return true
	}
	return len(htmlTagPattern.FindAllString(text, -1)) > 0
}

func stripReplyMarkup(text string) string {
	text = htmlTagPattern.ReplaceAllString(text, "")
	return strings.NewReplacer("```", "", "`", "", "**", "").Replace(text)
}

func replyLooksLikeJSON(text string) bool {
	trimmed := strings.TrimSpace(strings.Trim(strings.TrimSpace(text), "`"))
	if strings.HasPrefix(trimmed, "json") {
		trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "json"))
	}
	if (strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")) && json.Valid([]byte(trimmed)) {
		return true
	}
	return jsonKeyPattern.MatchString(text)
}

func splitSentences(line string) []string {
	var sentences []string
	runes := []rune(line)
	start := 0
	for index := 0; index < len(runes); index++ {
		if !strings.ContainsRune(".!?", runes[index]) {
			continue
		}
		end := index
		for end+1 < len(runes) && strings.ContainsRune(".!?", runes[end+1]) {
			end++
		}
		if end+1 == len(runes) || unicode.IsSpace(runes[end+1]) {
			for end+1 < len(runes) && unicode.IsSpace(runes[end+1]) {
				end++
			}
			sentences = append(sentences, string(runes[start:end+1]))
			start = end + 1
		}
		index = end
	}
	if start < len(runes) {
		sentences = append(sentences, string(runes[start:]))
	}
	return sentences
}

func brokenUnit(sentence string, last bool) bool {
	trimmed := strings.TrimSpace(sentence)
	if trimmed == "" {
		return false
	}
	if loneMarkPattern.MatchString(trimmed) || emptySlotPattern.MatchString(trimmed) || strings.Count(trimmed, "(") > strings.Count(trimmed, ")") {
		return true
	}
	return last && danglingTail.MatchString(trimmed)
}

func dropUnits(text string, bad func(sentence string, last bool) bool) (string, bool) {
	lines := strings.Split(text, "\n")
	lastLine := len(lines) - 1
	for lastLine > 0 && strings.TrimSpace(lines[lastLine]) == "" {
		lastLine--
	}
	kept := make([]string, 0, len(lines))
	for lineIndex, line := range lines {
		if strings.TrimSpace(line) == "" {
			kept = append(kept, line)
			continue
		}
		sentences := splitSentences(line)
		var remaining []string
		for index, sentence := range sentences {
			if !bad(sentence, lineIndex == lastLine && index == len(sentences)-1) {
				remaining = append(remaining, sentence)
			}
		}
		if len(remaining) > 0 {
			kept = append(kept, strings.TrimSpace(strings.Join(remaining, "")))
		}
	}
	repaired := strings.TrimSpace(strings.Join(kept, "\n"))
	return repaired, repaired != "" && hasLetters(repaired)
}

func hasBrokenUnit(text string) bool {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for lineIndex, line := range lines {
		sentences := splitSentences(line)
		for index, sentence := range sentences {
			if brokenUnit(sentence, lineIndex == len(lines)-1 && index == len(sentences)-1) {
				return true
			}
		}
	}
	return false
}

func hasLetters(text string) bool {
	for _, r := range text {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func repairInternalCodes(text string) (string, bool) {
	if uuidPattern.MatchString(text) || replyLooksLikeJSON(text) {
		return "", false
	}
	lower := strings.ToLower(text)
	repaired := plainlabel.UpperCodePattern.ReplaceAllStringFunc(text, func(code string) string {
		label, known := plainlabel.Label(code)
		if !known {
			return "\x00"
		}
		if strings.Contains(lower, strings.ToLower(label)) {
			return "\x01"
		}
		return label
	})
	if strings.Contains(repaired, "\x00") {
		return "", false
	}
	repaired = regexp.MustCompile(`\s*[\(\[]\x01[\)\]]`).ReplaceAllString(repaired, "")
	repaired = regexp.MustCompile(`\s*\x01\s*`).ReplaceAllString(repaired, " ")
	repaired = strings.TrimSpace(repaired)
	return repaired, repaired != "" && !plainlabel.UpperCodePattern.MatchString(repaired)
}

func guardReply(text, fallback string) (string, replyFault) {
	current := strings.TrimSpace(text)
	if current == "" {
		return fallback, replyFaultEmpty
	}
	fault := replyFaultNone
	if uuidPattern.MatchString(current) || replyLooksLikeJSON(current) || plainlabel.UpperCodePattern.MatchString(current) {
		repaired, ok := repairInternalCodes(current)
		if !ok {
			return fallback, replyFaultCode
		}
		current, fault = repaired, replyFaultCode
	}
	if mechanicsPattern.MatchString(current) {
		repaired, ok := dropUnits(current, func(sentence string, _ bool) bool { return mechanicsPattern.MatchString(sentence) })
		if !ok {
			return fallback, replyFaultMechanics
		}
		current = repaired
		if fault == replyFaultNone {
			fault = replyFaultMechanics
		}
	}
	if replyHasMarkupImbalance(current) {
		current = strings.TrimSpace(stripReplyMarkup(current))
		if fault == replyFaultNone {
			fault = replyFaultBroken
		}
	}
	if hasBrokenUnit(current) {
		repaired, ok := dropUnits(current, brokenUnit)
		if !ok {
			return fallback, replyFaultBroken
		}
		current = repaired
		if fault == replyFaultNone {
			fault = replyFaultBroken
		}
	}
	if current == "" || !hasLetters(current) {
		return fallback, replyFaultBroken
	}
	return current, fault
}

func (p *Processor) recordReplyRepaired(ctx context.Context, state *agentState, fault replyFault) {
	if p.pool == nil || state.HouseholdID == "" || fault == replyFaultNone {
		return
	}
	_, err := p.pool.Exec(ctx, `INSERT INTO product_telemetry_event(household_id,source_event_id,event_type,action,decision_source)
		VALUES($1,NULLIF($2,'')::uuid,'INTERACTION',$3,$4) ON CONFLICT DO NOTHING`,
		state.HouseholdID, state.SourceEventID, interactionReplyRepaired, string(fault))
	if err != nil {
		slog.Default().Warn("record reply repaired telemetry failed", "household_id", state.HouseholdID, "source_event_id", state.SourceEventID, "reason", string(fault), "error", err)
	}
}

func (p *Processor) finishModelText(ctx context.Context, state *agentState, text, fallback string) error {
	guarded, fault := guardReply(text, fallback)
	p.recordReplyRepaired(ctx, state, fault)
	return p.finishAgentText(ctx, state, guarded)
}

func replyFallbackFor(state *agentState, finalPhase bool) string {
	switch {
	case state.SideEffects > 0:
		return genericMutationReply
	case finalPhase:
		return noFindingsMessage
	}
	return replyFallbackMessage
}

func markRepeatedQuestion(turnContext map[string]any, repeated bool) {
	if repeated {
		turnContext["repeated_question"] = true
	}
}

func plainOpenCards(cards []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(cards))
	for _, card := range cards {
		plain := map[string]any{}
		for key, value := range card {
			switch key {
			case "review_type", "review_mode", "binding", "awaiting_field":
			default:
				plain[key] = plainlabel.Value(value)
			}
		}
		if kind, _ := card["review_type"].(string); kind != "" {
			if label, ok := plainlabel.Label(kind); ok {
				plain["topic"] = label
			} else {
				plain["topic"] = plainlabel.Humanize(kind)
			}
		}
		out = append(out, plain)
	}
	return out
}

func plainResult(result agentToolResult) agentToolResult {
	result.Facts = plainlabel.Facts(result.Facts)
	return result
}
