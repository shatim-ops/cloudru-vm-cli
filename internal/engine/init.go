package engine

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/shatim-ops/evo-cli/internal/confirm"
	clerrors "github.com/shatim-ops/evo-cli/internal/errors"
	"github.com/shatim-ops/evo-cli/internal/i18n"
	"github.com/shatim-ops/evo-cli/internal/provision"
)

// InitOptions holds parameters for the init command.
type InitOptions struct {
	Path           string // config file to create
	NonInteractive bool   // pick the first zone and the preferred image without asking
}

// Init writes a project config with an availability zone and a boot image
// taken from the Cloud.ru API, not from a list baked into the binary: zone
// and image UUIDs differ between tenants and change over time. Without a
// terminal (or with NonInteractive) it takes the preferred zone (else the
// first) and the newest plain Ubuntu (else Debian) image and prints what it
// chose.
func (e *Engine) Init(ctx context.Context, opts InitOptions) error {
	if _, err := os.Stat(opts.Path); err == nil {
		e.Printer.Warning(opts.Path + i18n.T(" already exists. Skipping.", " уже существует, файл не изменён."))
		return nil
	}
	// deploy reads this compose file; without it the first deploy fails
	// after the setup is done, so say it now. A warning: the file may come
	// later.
	if f := e.Config.ComposeFile; f != "" {
		if _, err := os.Stat(f); os.IsNotExist(err) {
			e.Printer.Warning(i18n.Tf("%s not found: 'evo deploy' needs it (another file: compose_file in %s or 'evo deploy -f')",
				"%s не найден: он нужен 'evo deploy' (другой файл — compose_file в %s или 'evo deploy -f')", f, opts.Path))
		}
	}

	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	e.Cloud.SetToken(tok.Value)

	zones, err := e.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return fmt.Errorf(i18n.T("list zones: %w", "список зон: %w"), err)
	}
	if len(zones) == 0 {
		return clerrors.New(clerrors.CodeAPIError, i18n.T("the API returned no availability zones", "API не вернул ни одной зоны доступности"),
			i18n.T("Check the project in the Cloud.ru console.", "Проверьте проект в консоли Cloud.ru."))
	}
	images, err := e.Cloud.ListImages(ctx)
	if err != nil {
		return fmt.Errorf(i18n.T("list images: %w", "список образов: %w"), err)
	}
	candidates := deployableImages(images)
	if len(candidates) == 0 {
		return clerrors.New(clerrors.CodeConfigInvalid,
			i18n.T("no Ubuntu or Debian image found in the image catalog", "в каталоге образов нет Ubuntu или Debian"),
			i18n.Tf("Pick an image with 'evo list-images' and set image_id in %s by hand.",
				"Выберите образ ('evo list-images') и впишите image_id в %s вручную.", opts.Path))
	}

	interactive := !opts.NonInteractive && confirm.StdinIsTTY()

	zones = preferredZoneFirst(zones)
	zoneNames := make([]string, len(zones))
	for i, z := range zones {
		zoneNames[i] = z.Name
	}
	zi, err := e.choose(ctx, interactive, i18n.T("Availability zone", "Зона доступности"), zoneNames)
	if err != nil {
		return err
	}
	zone := zones[zi]

	imageNames := make([]string, len(candidates))
	for i, img := range candidates {
		imageNames[i] = img.Name
	}
	ii, err := e.choose(ctx, interactive, i18n.T("Boot image", "Загрузочный образ"), imageNames)
	if err != nil {
		return err
	}
	image := candidates[ii]

	e.Printer.StepDone(i18n.Tf("Zone: %s (%s)", "Зона: %s (%s)", zone.Name, zone.ID))
	e.Printer.StepDone(i18n.Tf("Image: %s (%s)", "Образ: %s (%s)", image.Name, image.ID))

	projectLine := i18n.T("# project_id: <your-project-uuid>", "# project_id: <UUID вашего проекта>")
	if e.Config.ProjectID != "" {
		projectLine = "project_id: " + e.Config.ProjectID
	}
	content := fmt.Sprintf(i18n.T(initTemplateEN, initTemplateRU),
		yamlComment(zone.ID), yamlComment(zone.Name), yamlComment(image.ID), yamlComment(image.Name), projectLine)

	if err := os.WriteFile(opts.Path, []byte(content), 0o644); err != nil {
		return fmt.Errorf(i18n.T("failed to create config: %w", "не удалось создать файл настроек: %w"), err)
	}

	e.Printer.Success(i18n.T("Created ", "Создан ") + opts.Path)
	if e.Config.ProjectID == "" {
		e.Printer.Hint(i18n.Tf("Set project_id in %s, then run 'evo deploy'.", "Впишите project_id в %s и выполните 'evo deploy'.", opts.Path))
	} else {
		e.Printer.Hint(i18n.T("Run 'evo deploy'.", "Дальше: 'evo deploy'."))
	}
	return nil
}

// preferredZone is the default zone of init: offered first and taken
// without a terminal. 15.10.2026 ru.AZ-1 failed three cloud-e2e runs in a row
// (a VM 10–20 min in creating, error_creating, no SSH after boot) while
// ru.AZ-2 passed every one; the API lists ru.AZ-1 first.
const preferredZone = "ru.AZ-2"

// preferredZoneFirst moves preferredZone to the front, keeping the order of
// the rest; zones without it are returned as they are.
func preferredZoneFirst(zones []*provision.AvailabilityZone) []*provision.AvailabilityZone {
	for i, z := range zones {
		if z.Name == preferredZone {
			out := make([]*provision.AvailabilityZone, 0, len(zones))
			out = append(out, z)
			out = append(out, zones[:i]...)
			return append(out, zones[i+1:]...)
		}
	}
	return zones
}

// initTemplateEN and initTemplateRU are the generated project file; the
// arguments are the zone ID and name, the image ID and name, and the
// project_id line.
const initTemplateRU = `# Настройки evo
# Подробнее: https://github.com/shatim-ops/evo-cli

# Зона доступности (evo list-zones)
region: %s  # %s

# Загрузочный образ (evo list-images). Docker ставится при первом deploy.
image_id: %s  # %s

# Flavor ВМ: auto подбирает наименьший, которого хватит сервисам compose-файла
flavor: auto

# Размер загрузочного диска, ГБ (если меньше минимума образа, берётся минимум)
disk_size: 10

# Compose-файл для развёртывания
compose_file: docker-compose.yml

# Сколько секунд deploy ждёт, пока новая ВМ станет активной
timeout: 600

# Группа безопасности (имя или UUID), в которой deploy откроет порты compose.
# Без неё берётся группа Default зоны, иначе первая по имени — с предупреждением.
# security_group: Default_ru.AZ-2

# UUID проекта Cloud.ru (обязателен; console.cloud.ru или CLOUDRU_PROJECT_ID)
%s
`

const initTemplateEN = `# evo configuration
# See: https://github.com/shatim-ops/evo-cli

# Availability zone (evo list-zones)
region: %s  # %s

# Boot image (evo list-images). Docker is installed on first deploy.
image_id: %s  # %s

# VM flavor: auto picks the smallest that fits your compose services
flavor: auto

# Boot disk size in GB (raised to the image minimum if smaller)
disk_size: 10

# Docker Compose file to deploy
compose_file: docker-compose.yml

# Seconds deploy waits for a new VM to become active
timeout: 600

# Security group (name or UUID) deploy opens the compose ports in. Without it:
# the zone's Default group, else the first by name, with a warning.
# security_group: Default_ru.AZ-2

# Cloud.ru project UUID (required; console.cloud.ru or CLOUDRU_PROJECT_ID)
%s
`

// choose returns the index of the option picked by the user, or 0 when not
// interactive. An empty answer takes the first option.
func (e *Engine) choose(ctx context.Context, interactive bool, what string, options []string) (int, error) {
	if !interactive || len(options) == 1 {
		return 0, nil
	}
	w := e.Printer.ErrWriter()
	fmt.Fprintf(w, "%s:\n", what)
	for i, o := range options {
		fmt.Fprintf(w, "  %d) %s\n", i+1, o)
	}
	fmt.Fprint(w, i18n.T("Choose [1]: ", "Выберите [1]: "))
	answer, err := confirm.ReadLine(ctx)
	if err != nil {
		return 0, err
	}
	if answer == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(answer)
	if err != nil || n < 1 || n > len(options) {
		return 0, clerrors.New(clerrors.CodeUsage,
			i18n.Tf("invalid choice %q for %s", "недопустимый выбор %q: %s", answer, strings.ToLower(what)),
			i18n.Tf("Enter a number from 1 to %d, or re-run with --non-interactive.",
				"Введите число от 1 до %d или повторите с --non-interactive.", len(options)))
	}
	return n - 1, nil
}

// Plain distribution images ("Ubuntu-24.04", "ubuntu-22.04", "debian-12"):
// the Docker install script supports them. Marketplace builds, GPU and
// desktop variants carry suffixes and do not match.
var (
	ubuntuImageRe = regexp.MustCompile(`(?i)^ubuntu[-_ ]?(\d{2})\.(\d{2})$`)
	debianImageRe = regexp.MustCompile(`(?i)^debian[-_ ]?(\d{1,2})$`)
)

// deployableImages returns the images deploy can install Docker on, best
// first: Ubuntu by version descending, then Debian by version descending.
func deployableImages(images []*provision.Image) []*provision.Image {
	type ranked struct {
		img     *provision.Image
		family  int // 0 ubuntu, 1 debian
		version int
	}
	var out []ranked
	for _, img := range images {
		if !img.Usable() {
			continue
		}
		if m := ubuntuImageRe.FindStringSubmatch(img.Name); m != nil {
			major, _ := strconv.Atoi(m[1])
			minor, _ := strconv.Atoi(m[2])
			out = append(out, ranked{img, 0, major*100 + minor})
		} else if m := debianImageRe.FindStringSubmatch(img.Name); m != nil {
			v, _ := strconv.Atoi(m[1])
			out = append(out, ranked{img, 1, v})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].family != out[j].family {
			return out[i].family < out[j].family
		}
		return out[i].version > out[j].version
	})
	res := make([]*provision.Image, len(out))
	for i, r := range out {
		res[i] = r.img
	}
	return res
}

// yamlComment keeps a value from the cloud on its own line of the generated
// config: a zone or image name with a newline would otherwise add keys
// (compose_file, flavor, ...) to the user's project file.
func yamlComment(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
}
