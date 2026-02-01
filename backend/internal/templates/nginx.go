package templates

import (
	"bytes"
	"fmt"
	"text/template"
)

type NginxTemplateData struct {
	Domain       string
	FrontendPath string
	ProxyEnabled bool
	ProxyPort    int
}

const nginxTemplate = `server {
    listen 80;
    server_name {{.Domain}};

    # Gzip compression
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types text/plain text/css application/json application/javascript text/xml application/xml application/xml+rss text/javascript image/svg+xml;

{{- if .FrontendPath}}

    # Frontend - static files
    location / {
        root {{.FrontendPath}};
        index index.html;
        try_files $uri $uri/ /index.html;

        # Cache static assets
        location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
            expires 30d;
            add_header Cache-Control "public, immutable";
        }
    }
{{- end}}

{{- if .ProxyEnabled}}

    # WebSocket proxy
    location /ws {
        proxy_pass http://localhost:{{.ProxyPort}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_read_timeout 3600s;
        proxy_send_timeout 3600s;
    }

    # Backend - reverse proxy
    location /api/ {
        proxy_pass http://localhost:{{.ProxyPort}};
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header CF-Connecting-IP $http_cf_connecting_ip;

        # Timeouts
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;
    }
{{- end}}

    # Logging
    access_log /var/log/nginx/{{.Domain}}_access.log;
    error_log /var/log/nginx/{{.Domain}}_error.log;
}
`

func RenderNginxConfig(data NginxTemplateData) string {
	tmpl, err := template.New("nginx").Parse(nginxTemplate)
	if err != nil {
		return fmt.Sprintf("# Template error: %v", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Sprintf("# Render error: %v", err)
	}

	return buf.String()
}
