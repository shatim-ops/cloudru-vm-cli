package tools

import (
	"context"
	"fmt"

	"github.com/shatim-ops/evo-cli/internal/containerapp"
)

// caClient builds an authenticated Container Apps client.
func (s *Registry) caClient(ctx context.Context) (*containerapp.Client, error) {
	eng, err := s.newEngine(nil)
	if err != nil {
		return nil, err
	}
	tok, err := eng.Auth.Token(ctx)
	if err != nil {
		return nil, err
	}
	return containerapp.New(tok.Value, eng.Config.ProjectID, ""), nil
}

func (s *Registry) handleCaList(ctx context.Context, args map[string]any) (string, bool) {
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	items, err := cli.List(ctx)
	if err != nil {
		return errResult(err), true
	}
	out := make([]map[string]any, 0, len(items))
	for _, m := range items {
		out = append(out, map[string]any{"name": m["name"], "status": m["status"], "id": m["id"]})
	}
	return toJSON(map[string]any{"containers": out}), false
}

func (s *Registry) handleCaGet(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return errResult(fmt.Errorf("name is required")), true
	}
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	m, err := cli.Get(ctx, name)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(m), false
}

// handleCaDeploy creates+runs a container app (gated). Async.
func (s *Registry) handleCaDeploy(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	image := stringArg(args, "image")
	if name == "" || image == "" {
		return errResult(fmt.Errorf("name and image are required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("ca_deploy will create+run container app %q from image %q (billable). Call again with confirmed=true after the user approves.", name, image),
		}), true
	}

	cpu := stringArg(args, "cpu")
	if cpu == "" {
		cpu = "0.1"
	}
	memory := stringArg(args, "memory")
	if memory == "" {
		memory = "256Mi"
	}
	port := intArg(args, "port", 8080)
	public := true
	if v, ok := args["public"].(bool); ok {
		public = v
	}
	minI := intArg(args, "min_instances", 0)
	maxI := intArg(args, "max_instances", 1)

	// Container ports <= 1024 require privileged mode (platform rule).
	privileged := port <= 1024
	if v, ok := args["privileged"].(bool); ok {
		privileged = privileged || v
	}

	var env []containerapp.Env
	if raw, ok := args["env"].(map[string]any); ok {
		for k, v := range raw {
			env = append(env, containerapp.Env{Name: k, Value: fmt.Sprintf("%v", v)})
		}
	}

	req := containerapp.CreateContainerReq{
		Name:          name,
		Description:   stringArg(args, "description"),
		Configuration: &containerapp.Configuration{Ingress: &containerapp.Ingress{PubliclyAccessible: public}, Privileged: privileged},
		Template: containerapp.Template{
			Protocol: "http_1",
			Scaling:  &containerapp.Scaling{MinInstanceCount: minI, MaxInstanceCount: maxI},
			Containers: []containerapp.Container{{
				Name:          name,
				Image:         image,
				Resources:     &containerapp.Resources{CPU: cpu, Memory: memory},
				ContainerPort: port,
				Env:           env,
			}},
		},
	}

	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.Create(ctx, req)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{
		"status":       "deploying",
		"name":         name,
		"operation_id": op.ID,
		"note":         "Container app is deploying (async). Use ca_get for status + public URL, ca_logs for logs. Scales to zero when idle.",
	}), false
}

func (s *Registry) handleCaDelete(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return errResult(fmt.Errorf("name is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("ca_delete permanently deletes container app %q. Call again with confirmed=true after the user approves.", name),
		}), true
	}
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	op, err := cli.Delete(ctx, name)
	if err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "deleting", "name": name, "operation_id": op.ID}), false
}

func (s *Registry) handleCaLogs(ctx context.Context, args map[string]any) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return errResult(fmt.Errorf("name is required")), true
	}
	system, _ := args["system"].(bool)
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	logs, err := cli.Logs(ctx, name, system)
	if err != nil {
		return errResult(err), true
	}
	if len(logs) > 100 {
		logs = logs[len(logs)-100:]
	}
	return toJSON(map[string]any{"logs": logs}), false
}

func (s *Registry) handleCaStartStop(ctx context.Context, args map[string]any, action string) (string, bool) {
	name := stringArg(args, "name")
	if name == "" {
		return errResult(fmt.Errorf("name is required")), true
	}
	if confirmed, _ := args["confirmed"].(bool); !confirmed {
		effect := "it is billed while running"
		if action == "stop" {
			effect = "it stops serving requests"
		}
		return toJSON(map[string]any{
			"status":  "confirmation_required",
			"message": fmt.Sprintf("ca_%s will %s container app %q (%s). Call again with confirmed=true after the user approves.", action, action, name, effect),
		}), true
	}
	cli, err := s.caClient(ctx)
	if err != nil {
		return errResult(err), true
	}
	if err := cli.StartStop(ctx, name, action); err != nil {
		return errResult(err), true
	}
	return toJSON(map[string]any{"status": "ok", "name": name, "action": action}), false
}

func (s *Registry) handleCaStart(ctx context.Context, args map[string]any) (string, bool) {
	return s.handleCaStartStop(ctx, args, "start")
}
func (s *Registry) handleCaStop(ctx context.Context, args map[string]any) (string, bool) {
	return s.handleCaStartStop(ctx, args, "stop")
}
