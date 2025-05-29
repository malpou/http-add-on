# Placeholder Page Example

This example demonstrates how to use placeholder pages during scale-from-zero scenarios.

## Quick Start

### 1. Deploy a sample application with placeholder configuration

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: xkcd
spec:
  replicas: 0  # Start with zero replicas
  selector:
    matchLabels:
      app: xkcd
  template:
    metadata:
      labels:
        app: xkcd
    spec:
      containers:
      - name: xkcd
        image: registry.k8s.io/e2e-test-images/agnhost:2.45
        args:
        - netexec
        - --http-port=8080
        ports:
        - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: xkcd
spec:
  selector:
    app: xkcd
  ports:
  - port: 8080
    targetPort: 8080
```

### 2. Create a custom placeholder template

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: xkcd-placeholder
data:
  template.html: |
    <!DOCTYPE html>
    <html>
    <head>
        <title>XKCD Comics Loading...</title>
        <meta http-equiv="refresh" content="3">
        <style>
            body {
                font-family: 'Comic Sans MS', cursive;
                text-align: center;
                padding: 50px;
                background: #f0f0f0;
            }
            .comic-loader {
                font-size: 48px;
                animation: bounce 1s infinite;
            }
            @keyframes bounce {
                0%, 100% { transform: translateY(0); }
                50% { transform: translateY(-20px); }
            }
        </style>
    </head>
    <body>
        <h1>XKCD Comics</h1>
        <div class="comic-loader">📚</div>
        <p>Loading today's comic...</p>
        <p><small>Your comic will appear shortly!</small></p>
    </body>
    </html>
```

### 3. Create HTTPScaledObject with placeholder configuration

```yaml
apiVersion: http.keda.sh/v1alpha1
kind: HTTPScaledObject
metadata:
  name: xkcd
spec:
  hosts:
  - xkcd.example.com
  scaleTargetRef:
    name: xkcd
    service: xkcd
    port: 8080
  replicas:
    min: 0
    max: 10
  scalingMetric:
    concurrency:
      targetValue: 10
  placeholderConfig:
    enabled: true
    contentConfigMap: xkcd-placeholder
    statusCode: 503
    refreshInterval: 3
    headers:
      X-Comic-Status: "warming-up"
```

### 4. Create an Ingress to route traffic

```yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: xkcd
spec:
  ingressClassName: nginx
  rules:
  - host: xkcd.example.com
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: keda-add-ons-http-interceptor-proxy
            port:
              number: 8080
```

## Testing the Placeholder

1. Access the application when scaled to zero:
   ```bash
   curl -H "Host: xkcd.example.com" http://your-ingress-ip/
   ```

2. You should see the custom placeholder page with the bouncing book emoji

3. The page will refresh every 3 seconds

4. Once the pod is ready, you'll be automatically redirected to the actual application

## Different Placeholder Configurations

### Inline HTML

```yaml
placeholderConfig:
  enabled: true
  content: "<h1>Loading...</h1><meta http-equiv='refresh' content='5'>"
  statusCode: 503
```

### With WebSocket support (future enhancement)

```yaml
placeholderConfig:
  enabled: true
  contentConfigMap: websocket-placeholder
  headers:
    X-Placeholder-Mode: "websocket"
    Cache-Control: "no-cache"
```

### Minimal configuration

```yaml
placeholderConfig:
  enabled: true  # Uses default template
```

## Monitoring

Check if placeholder pages are being served:

```bash
# Check interceptor logs
kubectl logs -l app=keda-add-ons-http-interceptor | grep "serving placeholder"

# Check for placeholder header
curl -I -H "Host: xkcd.example.com" http://your-ingress-ip/ | grep X-KEDA-HTTP-Placeholder-Served
```