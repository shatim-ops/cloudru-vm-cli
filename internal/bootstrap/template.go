package bootstrap

import (
	"bytes"
	_ "embed"
	"text/template"

	"github.com/shatim-ops/cloudru-vm-cli/internal/errors"
)

//go:embed cloudinit.yaml.tmpl
var cloudInitTemplate string

// renderTemplate executes the embedded cloud-init template with the given config.
func renderTemplate(cfg Config) (string, error) {
	tmpl, err := template.New("cloudinit").Parse(cloudInitTemplate)
	if err != nil {
		return "", errors.Wrap(
			errors.CodeConfigInvalid,
			"failed to parse cloud-init template",
			"This is likely a bug; please report it",
			err,
		)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, cfg); err != nil {
		return "", errors.Wrap(
			errors.CodeConfigInvalid,
			"failed to render cloud-init template",
			"Check your config values for invalid characters",
			err,
		)
	}

	return buf.String(), nil
}
