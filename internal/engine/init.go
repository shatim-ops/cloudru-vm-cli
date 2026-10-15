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
// terminal (or with NonInteractive) it takes the first zone and the newest
// plain Ubuntu (else Debian) image and prints what it chose.
func (e *Engine) Init(ctx context.Context, opts InitOptions) error {
	if _, err := os.Stat(opts.Path); err == nil {
		e.Printer.Warning(opts.Path + " already exists. Skipping.")
		return nil
	}

	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	e.Cloud.SetToken(tok.Value)

	zones, err := e.Cloud.ListAvailabilityZones(ctx)
	if err != nil {
		return fmt.Errorf("list zones: %w", err)
	}
	if len(zones) == 0 {
		return clerrors.New(clerrors.CodeAPIError, "the API returned no availability zones", "Check the project in the Cloud.ru console.")
	}
	images, err := e.Cloud.ListImages(ctx)
	if err != nil {
		return fmt.Errorf("list images: %w", err)
	}
	candidates := deployableImages(images)
	if len(candidates) == 0 {
		return clerrors.New(clerrors.CodeConfigInvalid,
			"no Ubuntu or Debian image found in the image catalog",
			"Pick an image with 'evo list-images' and set image_id in "+opts.Path+" by hand.")
	}

	interactive := !opts.NonInteractive && confirm.StdinIsTTY()

	zoneNames := make([]string, len(zones))
	for i, z := range zones {
		zoneNames[i] = z.Name
	}
	zi, err := e.choose(ctx, interactive, "Availability zone", zoneNames)
	if err != nil {
		return err
	}
	zone := zones[zi]

	imageNames := make([]string, len(candidates))
	for i, img := range candidates {
		imageNames[i] = img.Name
	}
	ii, err := e.choose(ctx, interactive, "Boot image", imageNames)
	if err != nil {
		return err
	}
	image := candidates[ii]

	e.Printer.StepDone(fmt.Sprintf("Zone: %s (%s)", zone.Name, zone.ID))
	e.Printer.StepDone(fmt.Sprintf("Image: %s (%s)", image.Name, image.ID))

	projectLine := "# project_id: <your-project-uuid>"
	if e.Config.ProjectID != "" {
		projectLine = "project_id: " + e.Config.ProjectID
	}
	content := fmt.Sprintf(`# evo configuration
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

# Operation timeout in seconds
timeout: 300

# Cloud.ru project UUID (required; console.cloud.ru or CLOUDRU_PROJECT_ID)
%s
`, yamlComment(zone.ID), yamlComment(zone.Name), yamlComment(image.ID), yamlComment(image.Name), projectLine)

	if err := os.WriteFile(opts.Path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("failed to create config: %w", err)
	}

	e.Printer.Success("Created " + opts.Path)
	if e.Config.ProjectID == "" {
		e.Printer.Hint("Set project_id in " + opts.Path + ", then run 'evo deploy'.")
	} else {
		e.Printer.Hint("Run 'evo deploy'.")
	}
	return nil
}

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
	fmt.Fprintf(w, "Choose [1]: ")
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
			fmt.Sprintf("invalid choice %q for %s", answer, strings.ToLower(what)),
			fmt.Sprintf("Enter a number from 1 to %d, or re-run with --non-interactive.", len(options)))
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
		switch strings.ToLower(img.Status) {
		case "deleted", "error", "creating":
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
