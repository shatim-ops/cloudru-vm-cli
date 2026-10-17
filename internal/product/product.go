// Package product holds the product's display name. The binary, the MCP
// server name, the environment variables and the config paths stay "evo".
package product

// Name is shown in evo --help, evo version, the MCP serverInfo title and
// the release name (.goreleaser.yaml). The edition is for testing inside
// Cloud.ru and collecting feedback.
const Name = "Cloud.ru Evolution CLI CREW edition"

// Feedback goes to the author and the testers' chat in Telegram; shown in
// evo --help and README.
const (
	FeedbackContact = "https://t.me/shatim"
	FeedbackChat    = "https://t.me/+SYvXziA4G9c1Y2Ni"
)
