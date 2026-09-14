package mcp

import "testing"

// Адрес реестра API не возвращает, а собрать его из имени по соглашению
// платформы должен движок, а не каждый вызывающий: угадавший неверно толкает
// образ продукта в никуда и узнаёт об этом на установке в кластер.
func TestАдресРеестраВыводитсяИзИмени(t *testing.T) {
	if got := registryURL(map[string]any{"name": "marketplace-factory"}); got != "marketplace-factory.cr.cloud.ru" {
		t.Fatalf("получили %q", got)
	}
	// Если API однажды начнёт отдавать адрес сам — верим ему, а не соглашению.
	if got := registryURL(map[string]any{"name": "x", "url": "x.ar.cloud.ru"}); got != "x.ar.cloud.ru" {
		t.Fatalf("адрес из API должен побеждать соглашение, получили %q", got)
	}
	// Без имени выдумывать адрес нельзя.
	if got := registryURL(map[string]any{}); got != "" {
		t.Fatalf("без имени ждали пустую строку, получили %q", got)
	}
}
