package provider

import (
	"fmt"
	"net/http"
	"strings"
)

// fakeMCS is the container host part of the fake API. Like the real one: a host is "ready" on
// the second look, a deployment finishes on the second look (it fails when the compose file
// mentions "image: nope"), a plan change is applied on the second look, a deleted host is gone
// on the second look, and live calls need a ready host.
type fakeMCS struct {
	hosts       map[string]map[string]any
	env         map[string]map[string]string // host id -> env
	deployments map[string][]map[string]any  // host id -> deployments, oldest first
	polls       map[string]int
	failInstall bool             // new hosts end "failed"
	registries  []map[string]any // Container Hub registries (GET /v1/registries)
}

func newFakeMCS() *fakeMCS {
	return &fakeMCS{hosts: map[string]map[string]any{}, env: map[string]map[string]string{},
		deployments: map[string][]map[string]any{}, polls: map[string]int{},
		registries: []map[string]any{
			{"id": "reg-1", "name": "ai-platform", "region": "dhaka-1", "endpoint": "cch.example.com/ai-platform-x", "status": "active",
				"project_id": "p1", "harbor_robot_token": "secret", "harbor_robot_name": "robot$ai-platform-x+r"},
			{"id": "reg-2", "name": "other", "region": "dhaka-1", "endpoint": "hub.example.net/other-y", "status": "active", "project_id": "p1"},
		}}
}

var mcsPlans = []map[string]any{
	{"slug": "s-1vcpu-2gb", "cpu": 1, "memory_gb": 2.0, "disk_gb": 50, "server_per_month": 999.0, "fee_per_month": 300.0, "per_month": 1299.0, "per_hour": 1.8042},
	{"slug": "s-2vcpu-4gb", "cpu": 2, "memory_gb": 4.0, "disk_gb": 80, "server_per_month": 1799.0, "fee_per_month": 400.0, "per_month": 2199.0, "per_hour": 3.0542},
}

func (m *fakeMCS) lastDeployment(id string) any {
	deps := m.deployments[id]
	if len(deps) == 0 {
		return nil
	}
	d := deps[len(deps)-1]
	return map[string]any{"id": d["id"], "number": d["number"], "status": d["status"]}
}

func (m *fakeMCS) view(h map[string]any, detail bool) map[string]any {
	out := map[string]any{}
	for k, v := range h {
		if k == "compose" && !detail {
			continue
		}
		out[k] = v
	}
	out["last_deployment"] = m.lastDeployment(h["id"].(string))
	if detail {
		keys := []string{}
		for k := range m.env[h["id"].(string)] {
			keys = append(keys, k)
		}
		out["env_keys"] = keys
	}
	return out
}

func (f *fakeAPI) containerHosts(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any) {
	m := f.mcs
	if len(parts) == 2 && parts[1] == "options" {
		reply(w, 200, map[string]any{"enabled": true, "plans": mcsPlans})
		return
	}
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			out := []map[string]any{}
			for _, h := range m.hosts {
				out = append(out, m.view(h, false))
			}
			reply(w, 200, out)
		case "POST":
			plan, _ := body["plan"].(string)
			known := false
			for _, p := range mcsPlans {
				known = known || p["slug"] == plan
			}
			if !known {
				reply(w, 422, map[string]any{"detail": fmt.Sprintf("Unknown plan '%s'", plan), "code": "validation_error"})
				return
			}
			str := func(k, def string) string {
				if v, _ := body[k].(string); v != "" {
					return v
				}
				return def
			}
			id := f.id("mcs")
			ip := fmt.Sprintf("203.0.113.%d", 100+f.seq)
			ipType, ingress, portainer := str("ip_type", "public"), ip, strings.ReplaceAll(ip, ".", "-")+".sslip.io"
			if ipType == "nat" {
				ip = fmt.Sprintf("172.16.0.%d", 100+f.seq)
				ingress, portainer = "103.143.148.30", "mcs-"+id+".103-143-148-30.sslip.io"
			}
			h := map[string]any{"id": id, "name": body["name"], "plan": plan, "status": "creating", "error": nil,
				"project_id": str("project_id", "p1"), "instance_id": "i-" + id, "ip_type": ipType, "ingress_ip": ingress,
				"server":        map[string]any{"status": "creating", "ip_address": ip, "region": str("region", "dhaka-1"), "cpu": 2, "memory": 4096, "disk": 80},
				"portainer_url": "https://" + portainer, "domains": []any{}, "registry_ids": []any{},
				"compose": nil, "created_at": "2026-11-20T10:00:00+00:00"}
			m.hosts[id] = h
			m.env[id] = map[string]string{}
			reply(w, 201, m.view(h, true))
		}
		return
	}
	id := parts[1]
	h, ok := m.hosts[id]
	if !ok {
		reply(w, 404, map[string]any{"detail": "Container host not found", "code": "not_found"})
		return
	}
	server := h["server"].(map[string]any)
	ready := func() bool {
		if h["status"] != "ready" || server["status"] != "running" {
			reply(w, 409, map[string]any{"detail": "The host is not ready yet", "code": "conflict"})
			return false
		}
		return true
	}
	deploy := func(compose any, note string) {
		if compose == nil || compose == "" {
			compose = h["compose"]
		}
		if compose == nil {
			reply(w, 422, map[string]any{"detail": "Add a compose file first", "code": "validation_error"})
			return
		}
		for _, d := range m.deployments[id] {
			if d["status"] == "running" {
				reply(w, 409, map[string]any{"detail": fmt.Sprintf("Deployment #%v is still running", d["number"]), "code": "conflict"})
				return
			}
		}
		keys := []string{}
		for k := range m.env[id] {
			keys = append(keys, k)
		}
		h["compose"] = compose
		dep := map[string]any{"id": f.id("dep"), "number": len(m.deployments[id]) + 1, "status": "running", "note": note,
			"env_keys": keys, "domains": h["domains"], "compose": compose, "output": "Creating app-web-1\n"}
		m.deployments[id] = append(m.deployments[id], dep)
		m.polls[dep["id"].(string)] = 0
		reply(w, 202, dep)
	}
	switch {
	case len(parts) == 2 && r.Method == "GET":
		m.polls[id]++
		if m.polls[id] >= 2 {
			switch h["status"] {
			case "creating", "installing":
				if m.failInstall {
					h["status"], h["error"], h["log_tail"] = "failed", "The installer failed (exit 100)", "E: Unable to locate package docker-ce\n"
					break
				}
				h["status"], server["status"], h["ready_at"] = "ready", "running", "2026-11-20T10:05:00+00:00"
			case "deleting":
				delete(m.hosts, id)
				reply(w, 404, map[string]any{"detail": "Container host not found", "code": "not_found"})
				return
			}
			if server["status"] == "resizing" {
				h["plan"], server["status"], h["resize"] = h["resize"].(map[string]any)["to"], "running", nil
			}
		}
		reply(w, 200, m.view(h, true))
	case len(parts) == 2 && r.Method == "DELETE":
		h["status"] = "deleting"
		m.polls[id] = 0
		reply(w, 200, map[string]any{"message": "Deleting the host and its server"})
	case len(parts) == 3 && parts[2] == "plan":
		if !ready() {
			return
		}
		if body["plan"] == h["plan"] {
			reply(w, 422, map[string]any{"detail": "The host is already on this plan", "code": "validation_error"})
			return
		}
		server["status"], h["resize"] = "resizing", map[string]any{"to": body["plan"], "error": nil}
		m.polls[id] = 0
		reply(w, 202, m.view(h, true))
	case len(parts) == 3 && parts[2] == "env" && r.Method == "GET":
		reply(w, 200, map[string]any{"env": m.env[id]})
	case len(parts) == 3 && parts[2] == "env":
		env := map[string]string{}
		for k, v := range body["env"].(map[string]any) {
			env[k] = v.(string)
		}
		m.env[id] = env
		keys := []string{}
		for k := range env {
			keys = append(keys, k)
		}
		if body["deploy"] == true && h["compose"] != nil {
			if ready() {
				deploy(nil, "Environment variables changed")
			}
			return
		}
		reply(w, 200, map[string]any{"env_keys": keys, "deployment": nil})
	case len(parts) == 3 && parts[2] == "domains":
		domains := []any{}
		for _, raw := range body["domains"].([]any) {
			d := raw.(map[string]any)
			domains = append(domains, map[string]any{"domain": strings.ToLower(d["domain"].(string)), "service": d["service"], "port": d["port"]})
		}
		h["domains"] = domains
		reply(w, 200, map[string]any{"domains": domains, "deployment": nil})
	case len(parts) == 3 && parts[2] == "registries":
		ids := []any{}
		for _, raw := range body["registry_ids"].([]any) {
			known := false
			for _, reg := range m.registries {
				known = known || reg["id"] == raw
			}
			if !known {
				reply(w, 400, map[string]any{"detail": "Choose active Container Hub registries of your organization.", "code": "bad_request"})
				return
			}
			ids = append(ids, raw)
		}
		h["registry_ids"] = ids
		reply(w, 200, map[string]any{"registry_ids": ids, "deployment": nil})
	case len(parts) == 3 && parts[2] == "deploy":
		if ready() {
			note, _ := body["note"].(string)
			deploy(body["compose"], note)
		}
	case len(parts) == 3 && parts[2] == "deployments":
		out := []map[string]any{}
		for i := len(m.deployments[id]) - 1; i >= 0; i-- {
			out = append(out, m.deployments[id][i])
		}
		reply(w, 200, out)
	case len(parts) == 4 && parts[2] == "deployments" && r.Method == "GET":
		for _, d := range m.deployments[id] {
			if d["id"] != parts[3] {
				continue
			}
			did := d["id"].(string)
			m.polls[did]++
			if d["status"] == "running" && m.polls[did] >= 2 {
				if strings.Contains(d["compose"].(string), "image: nope") {
					d["status"], d["output"] = "failed", "Pulling web\nError response from daemon: pull access denied for nope\n"
				} else {
					d["status"], d["output"] = "succeeded", "Creating app-web-1\nStarted app-web-1\n"
				}
				d["finished_at"] = "2026-11-20T10:06:00+00:00"
			}
			reply(w, 200, d)
			return
		}
		reply(w, 404, map[string]any{"detail": "Deployment not found", "code": "not_found"})
	default:
		notFound(w)
	}
}
