package report

import (
	"regexp"
	"strings"
)

// Reports get pasted into pull requests, and Entire's own redaction of
// transcripts is best effort. So every excerpt that reaches a report goes
// through a second pass here.
//
// This is pattern based and cannot catch every shape. That limit is stated in
// the report footer rather than hidden, because a scrubber that is trusted
// more than it deserves is worse than one nobody relies on.
var secretPatterns = []*regexp.Regexp{
	// GitHub tokens: classic, fine-grained, OAuth, app, refresh.
	regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{16,}\b`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}\b`),
	// AWS access key ids and their secret pairs.
	regexp.MustCompile(`\b(?:AKIA|ASIA|ABIA|ACCA)[0-9A-Z]{16}\b`),
	regexp.MustCompile(`(?i)\baws_secret_access_key\s*[=:]\s*\S+`),
	// Slack.
	regexp.MustCompile(`\bxox[baprs]-[A-Za-z0-9-]{10,}\b`),
	// OpenAI and Anthropic style keys.
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}\b`),
	regexp.MustCompile(`\bsk-ant-[A-Za-z0-9_-]{20,}\b`),
	// JWTs: three base64url segments.
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`),
	// Google API keys.
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`),
	// Private key blocks.
	regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`),
	// Generic key, token, secret and password assignments. Deliberately last,
	// so a more specific pattern gets to name the shape first.
	regexp.MustCompile(`(?i)\b(api[_-]?key|secret|token|password|passwd|bearer|authorization)\b\s*[=:]\s*["']?[A-Za-z0-9_\-./+=]{8,}["']?`),
	// An HTTP auth scheme followed by its credential. The generic assignment
	// pattern below cannot catch this, because the value follows a space
	// rather than an = or a colon.
	regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9_\-./+=]{8,}`),
	// URLs carrying credentials.
	regexp.MustCompile(`\b[a-z][a-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`),
}

// redacted is what replaces a match.
const redacted = "[redacted]"

// Scrub replaces anything that looks like a credential.
//
// Applied to every evidence excerpt before it is rendered or written, in the
// table, the JSON and the HTML alike, so no surface can leak what another
// surface hides.
func Scrub(s string) string {
	if s == "" {
		return s
	}
	for _, re := range secretPatterns {
		s = re.ReplaceAllString(s, redacted)
	}
	return s
}

// ScrubAll scrubs a slice in place-safe fashion.
func ScrubAll(in []string) []string {
	if len(in) == 0 {
		return in
	}
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = Scrub(s)
	}
	return out
}

// looksRedacted reports whether a string still carries a plausible secret
// after scrubbing. Used only by tests, to keep the patterns honest.
func looksRedacted(s string) bool {
	return strings.Contains(s, redacted)
}
