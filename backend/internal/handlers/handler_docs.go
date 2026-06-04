package handlers

import (
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

// RegisterDocsRoutes exposes an authenticated OpenAPI document generated from
// the Gin route table. It intentionally stays data-driven so newly registered
// routes appear in the spec without hand-maintained annotations drifting.
func RegisterDocsRoutes(r *gin.Engine, authMiddleware gin.HandlerFunc, version string) {
	docs := r.Group("/api/docs")
	if authMiddleware != nil {
		docs.Use(authMiddleware)
	}
	docs.GET("", swaggerUIHandler)
	docs.GET("/", swaggerUIHandler)
	docs.GET("/openapi.json", func(c *gin.Context) {
		c.JSON(http.StatusOK, buildOpenAPISpec(r.Routes(), version))
	})
}

func swaggerUIHandler(c *gin.Context) {
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.String(http.StatusOK, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>NovelBuilder API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
  <style>
    body { margin: 0; background: #f6f7fb; }
    .swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    const params = new URLSearchParams(window.location.search);
    const token = params.get("token");
    const specUrl = token
      ? "/api/docs/openapi.json?token=" + encodeURIComponent(token)
      : "/api/docs/openapi.json";
    window.ui = SwaggerUIBundle({
      url: specUrl,
      dom_id: "#swagger-ui",
      deepLinking: true,
      presets: [SwaggerUIBundle.presets.apis],
      requestInterceptor: (req) => {
        if (token) req.headers.Authorization = "Bearer " + token;
        return req;
      },
    });
  </script>
</body>
</html>`)
}

func buildOpenAPISpec(routes gin.RoutesInfo, version string) gin.H {
	paths := gin.H{}
	sorted := append(gin.RoutesInfo(nil), routes...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Path == sorted[j].Path {
			return sorted[i].Method < sorted[j].Method
		}
		return sorted[i].Path < sorted[j].Path
	})

	for _, route := range sorted {
		if !strings.HasPrefix(route.Path, "/api/") {
			continue
		}
		if route.Method == http.MethodHead || route.Method == http.MethodOptions {
			continue
		}
		path := openAPIPath(route.Path)
		item, _ := paths[path].(gin.H)
		if item == nil {
			item = gin.H{}
			paths[path] = item
		}
		item[strings.ToLower(route.Method)] = gin.H{
			"summary":     routeSummary(route),
			"operationId": operationID(route),
			"tags":        []string{routeTag(route.Path)},
			"responses": gin.H{
				"200": gin.H{"description": "OK"},
				"400": gin.H{"description": "Bad Request"},
				"401": gin.H{"description": "Unauthorized"},
				"500": gin.H{"description": "Internal Server Error"},
			},
		}
	}

	return gin.H{
		"openapi": "3.0.3",
		"info": gin.H{
			"title":       "NovelBuilder API",
			"version":     version,
			"description": "Authenticated runtime route index generated from the Gin router.",
		},
		"servers": []gin.H{{"url": "/"}},
		"security": []gin.H{
			{"bearerAuth": []string{}},
		},
		"components": gin.H{
			"securitySchemes": gin.H{
				"bearerAuth": gin.H{
					"type":         "http",
					"scheme":       "bearer",
					"bearerFormat": "session token",
				},
			},
		},
		"paths": paths,
	}
}

func openAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") && len(part) > 1 {
			parts[i] = "{" + part[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

func routeTag(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 2 {
		return "api"
	}
	if parts[1] == "projects" && len(parts) > 3 {
		return parts[3]
	}
	return parts[1]
}

func routeSummary(route gin.RouteInfo) string {
	return route.Method + " " + route.Path
}

func operationID(route gin.RouteInfo) string {
	replacer := strings.NewReplacer("/", "_", ":", "", "-", "_", "{", "", "}", "")
	id := strings.Trim(replacer.Replace(route.Method+"_"+route.Path), "_")
	return strings.ToLower(id)
}
