package kubetools

import (
	"slices"
	"testing"
)

func TestChildEnvDropsServerSecrets(t *testing.T) {
	got := childEnv([]string{
		"PATH=/usr/bin",
		"CLOUDRU_KEY_ID=k",
		"CLOUDRU_SECRET=s",
		"CLOUDRU_S3_SECRET_KEY=s3",
		"MCP_BEARER_TOKEN=t",
		"HOME=/home/u",
		"HTTPS_PROXY=http://proxy:3128",
	})
	want := []string{"PATH=/usr/bin", "HOME=/home/u", "HTTPS_PROXY=http://proxy:3128"}
	if !slices.Equal(got, want) {
		t.Fatalf("childEnv = %v, want %v", got, want)
	}
}
