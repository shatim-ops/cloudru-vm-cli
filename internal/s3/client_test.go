package s3

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Срок жизни подписанной ссылки ограничен семью сутками самим SigV4. Отвергать
// большее надо здесь и словами: иначе отказ приходит из S3 в виде
// невразумительной ошибки подписи, уже после того, как ссылку пообещали партнёру.
func TestPresignДольшеСемиСутокОтвергается(t *testing.T) {
	c, err := New(Config{AccessKey: "ak", SecretKey: "sk", Endpoint: "https://s3.example"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.PresignGet(context.Background(), "b", "k", 8*24*time.Hour)
	if err == nil || !strings.Contains(err.Error(), "7 days") {
		t.Fatalf("ждали отказ по сроку, получили %v", err)
	}
}

// Ссылка должна вести на тот объект, который просили, и нести подпись.
func TestPresignДаётПодписаннуюСсылку(t *testing.T) {
	c, err := New(Config{AccessKey: "ak", SecretKey: "sk", Endpoint: "https://s3.example"})
	if err != nil {
		t.Fatal(err)
	}
	u, err := c.PresignGet(context.Background(), "marketplace-factory", "qdrant/1.19.1/addon.tar.gz", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"marketplace-factory", "qdrant/1.19.1/addon.tar.gz", "X-Amz-Signature", "X-Amz-Expires"} {
		if !strings.Contains(u, want) {
			t.Errorf("в ссылке нет %q: %s", want, u)
		}
	}
}

// Неверный тип у бинарного артефакта заставляет часть клиентов «чинить»
// переводы строк, а это порча образа; по умолчанию — octet-stream.
func TestТипСодержимогоПоРасширению(t *testing.T) {
	cases := map[string]string{
		"a/b/image.raw":       "application/octet-stream",
		"addon.tar.gz":        "application/gzip",
		"addon.tgz":           "application/gzip",
		"manifest.json":       "application/json",
		"SHA256SUMS":          "application/octet-stream",
		"report.txt":          "text/plain; charset=utf-8",
		"values.yaml":         "application/yaml",
		"icon.svg":            "image/svg+xml",
		"archive.zip":         "application/zip",
		"docker-image.tar":    "application/x-tar",
		"strange.UNKNOWN-EXT": "application/octet-stream",
	}
	for path, want := range cases {
		if got := contentTypeFor(path); got != want {
			t.Errorf("%s: ждали %q, получили %q", path, want, got)
		}
	}
}

func TestБезКредовКлиентНеСоздаётся(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("клиент без ключей не должен создаваться")
	}
}
