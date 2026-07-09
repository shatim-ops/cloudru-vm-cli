package engine

import "context"

// ListImages fetches and displays available boot images from the Cloud.ru API.
func (e *Engine) ListImages(ctx context.Context) error {
	tok, err := e.Auth.Token(ctx)
	if err != nil {
		return err
	}
	e.Cloud.SetToken(tok.Value)

	images, err := e.Cloud.ListImages(ctx)
	if err != nil {
		return err
	}

	if len(images) == 0 {
		e.Printer.Success("No images found.")
		return nil
	}

	if e.Printer.IsJSON() {
		return e.Printer.JSON(images)
	}

	headers := []string{"ID", "Name", "OS", "Status"}
	rows := make([][]string, 0, len(images))
	for _, img := range images {
		rows = append(rows, []string{
			img.ID,
			img.Name,
			img.OS,
			img.Status,
		})
	}

	e.Printer.Table(headers, rows)
	return nil
}
