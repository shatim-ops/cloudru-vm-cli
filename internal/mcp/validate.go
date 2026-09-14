package mcp

import (
	"fmt"
	"regexp"
	"strings"
)

// Проверки аргументов, которые уходят в argv внешних инструментов.
//
// Оболочки здесь нет — kubectl и helm запускаются напрямую, — поэтому точки с
// запятой и кавычки безвредны. Опасно другое: аргумент, начинающийся с дефиса,
// инструмент разберёт как ФЛАГ. Для действия под гейтом это значит, что
// оператор подтвердил одно, а исполнилось другое: «установить релиз r» с
// release=--dry-run превращается в холостой прогон, а args с --kubeconfig
// уводит чтение в чужой кластер.

var (
	// имяРелиза — то, что принимает helm: DNS-1123 с точками, до 53 символов.
	имяРелиза = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]*[a-z0-9])?$`)
	// имяПространства — метка DNS-1123.
	имяПространства = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

	// мутирующиеПодкоманды — подкоманды разрешённых «на чтение» глаголов
	// kubectl, которые на самом деле меняют кластер.
	//
	// Гейт проверял только первое слово, и `rollout restart` (перезапуск подов
	// продукта), `rollout undo` (откат релиза), `auth reconcile` (перезапись
	// RBAC) проходили как чтение. Это не теория: именно так агент «посмотреть
	// статус» перезапускает боевую нагрузку без единого подтверждения.
	мутирующиеПодкоманды = map[string]map[string]bool{
		"rollout": {"restart": true, "undo": true, "pause": true, "resume": true},
		"auth":    {"reconcile": true},
		"config": {
			"set": true, "set-context": true, "set-cluster": true,
			"set-credentials": true, "unset": true, "delete-context": true,
			"delete-cluster": true, "delete-user": true, "rename-context": true,
			"use-context": true,
		},
	}
)

// проверитьАргументыKubectl отвергает то, что превращает чтение в чужое
// действие, и сообщает, требуется ли подтверждение.
func проверитьАргументыKubectl(поля []string) (нуженГейт bool, err error) {
	if len(поля) == 0 {
		return false, fmt.Errorf("'args' пуст")
	}
	verb := поля[0]
	if strings.HasPrefix(verb, "-") {
		return false, fmt.Errorf("первый аргумент %q — флаг, а не команда kubectl", verb)
	}
	// Подмена kubeconfig увела бы вызов в другой кластер, мимо того, который
	// одобрил оператор.
	for _, а := range поля {
		if а == "--kubeconfig" || strings.HasPrefix(а, "--kubeconfig=") {
			return false, fmt.Errorf("--kubeconfig задавать нельзя: движок подставляет конфиг одобренного кластера")
		}
	}
	if !readOnlyKubectl[verb] {
		return true, nil
	}
	if под, есть := мутирующиеПодкоманды[verb]; есть && len(поля) > 1 && под[поля[1]] {
		return true, nil
	}
	return false, nil
}

// проверитьИменаHelm отвергает имена, которые helm примет за флаги.
func проверитьИменаHelm(release, namespace, chart, chartPath, valuesFile string) error {
	if release != "" {
		if len(release) > 53 || !имяРелиза.MatchString(release) {
			return fmt.Errorf("release %q недопустим: helm ждёт имя DNS-1123 до 53 символов "+
				"(значение с дефиса он разберёт как флаг и сделает не то, что одобрили)", release)
		}
	}
	if namespace != "" {
		if len(namespace) > 63 || !имяПространства.MatchString(namespace) {
			return fmt.Errorf("namespace %q недопустим: нужна метка DNS-1123", namespace)
		}
	}
	for имя, значение := range map[string]string{
		"chart": chart, "chart_path": chartPath, "values_file": valuesFile,
	} {
		if strings.HasPrefix(значение, "-") {
			return fmt.Errorf("%s %q начинается с дефиса — helm разберёт его как флаг", имя, значение)
		}
	}
	return nil
}

// проверитьКлючS3 отвергает ключ, выходящий за пределы своего префикса.
//
// В бакете публикации лежат артефакты всех продуктов, разложенные по
// префиксам. Ключ с «..» или ведущим слэшем пишет мимо своего места — в чужой
// продукт или в корень.
func проверитьКлючS3(key string) error {
	if key == "" {
		return fmt.Errorf("'key' пуст")
	}
	if strings.HasPrefix(key, "/") {
		return fmt.Errorf("ключ %q начинается со слэша — у объектов S3 нет корня", key)
	}
	for _, часть := range strings.Split(key, "/") {
		if часть == ".." {
			return fmt.Errorf("ключ %q содержит «..» — запись ушла бы мимо своего префикса", key)
		}
	}
	if strings.ContainsAny(key, "\n\r\x00") {
		return fmt.Errorf("ключ %q содержит управляющий символ", key)
	}
	return nil
}

// СрокСсылкиМакс — потолок SigV4.
const СрокСсылкиМакс = 7 * 24 * 60 * 60

// проверитьСрокСсылки отвергает срок вне допустимого.
//
// Ноль и отрицательное молча превращались в максимум: попросили ссылку на час,
// получили на неделю, и доступ к продукту живёт дольше, чем считает оператор.
func проверитьСрокСсылки(secs int) error {
	if secs <= 0 {
		return fmt.Errorf("expires_seconds=%d: срок должен быть положительным", secs)
	}
	if secs > СрокСсылкиМакс {
		return fmt.Errorf("expires_seconds=%d: подписанная ссылка живёт не дольше 7 суток (%d секунд) — предел SigV4",
			secs, СрокСсылкиМакс)
	}
	return nil
}
