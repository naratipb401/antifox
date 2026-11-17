---
name: container-security-testing
description: Professional techniques and methodology for container security testing
version: 1.0.0
---

# Container Security Testing

## Overview

Container security testing is a critical part of securing containerized applications. This skill covers the methods, tools, and best practices for container security testing, including Docker, Kubernetes, and other container technologies.

## Testing Scope

### 1. Image Security

**Checklist:**
- Base image vulnerabilities
- Dependency package vulnerabilities
- Image configuration
- Sensitive data

### 2. Runtime Security

**Checklist:**
- Container privileges
- Resource limits
- Network isolation
- Filesystem

### 3. Orchestration Security

**Checklist:**
- Kubernetes configuration
- Service accounts
- RBAC
- Network policies

## Docker Security Testing

### Image Scanning

**Using Trivy:**
```bash
# Scan an image
trivy image nginx:latest

# Scan a local image tarball
trivy image --input nginx.tar

# Show only high-severity vulnerabilities
trivy image --severity HIGH,CRITICAL nginx:latest
```

**Using Clair:**
```bash
# Start Clair
docker run -d --name clair clair:latest

# Scan an image
clair-scanner --ip 192.168.1.100 nginx:latest
```

**Using Docker Bench:**
```bash
# Run the Docker security benchmark
docker run --rm --net host --pid host --userns host --cap-add audit_control \
  -e DOCKER_CONTENT_TRUST=$DOCKER_CONTENT_TRUST \
  -v /etc:/etc:ro \
  -v /usr/bin/containerd:/usr/bin/containerd:ro \
  -v /usr/bin/runc:/usr/bin/runc:ro \
  -v /usr/lib/systemd:/usr/lib/systemd:ro \
  -v /var/lib:/var/lib:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  --label docker_bench_security \
  docker/docker-bench-security
```

### Container Configuration Checks

**Check the Dockerfile:**
```dockerfile
# Example of security issues
FROM ubuntu:latest  # Uses the latest tag
RUN apt-get update && apt-get install -y curl  # No version pinned
COPY . /app  # May include sensitive files
ENV PASSWORD=secret  # Hardcoded password
USER root  # Runs as root
```

**Security best practices:**
```dockerfile
# Use a specific version
FROM ubuntu:20.04

# Pin package versions
RUN apt-get update && apt-get install -y curl=7.68.0-1ubuntu2.7

# Use a non-root user
RUN useradd -m appuser
USER appuser

# Minimize the image
FROM alpine:3.15

# Multi-stage build
FROM golang:1.18 AS builder
WORKDIR /app
COPY . .
RUN go build -o app

FROM alpine:3.15
COPY --from=builder /app/app /app
```

### Runtime Checks

**Check container privileges:**
```bash
# Check for privileged containers
docker ps --filter "label=privileged=true"

# Check mounted host directories
docker inspect container_name | grep -A 10 Mounts

# Check container networking
docker network inspect network_name
```

**Check resource limits:**
```bash
# Check memory limits
docker stats container_name

# Check CPU limits
docker inspect container_name | grep -i cpu
```

## Kubernetes Security Testing

### Configuration Checks

**Using kube-bench:**
```bash
# Run kube-bench
kube-bench run

# Check specific benchmarks
kube-bench run --targets master,node,etcd
```

**Using kube-hunter:**
```bash
# Run kube-hunter
kube-hunter --remote target-ip

# Active mode
kube-hunter --active
```

### Pod Security

**Check Pod security policies:**
```yaml
# Insecure Pod configuration
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: app
    image: nginx
    securityContext:
      privileged: true  # Privileged mode
      runAsUser: 0  # root user
```

**Hardened configuration:**
```yaml
apiVersion: v1
kind: Pod
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 2000
  containers:
  - name: app
    image: nginx
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop:
        - ALL
        add:
        - NET_BIND_SERVICE
```

### RBAC Checks

**Check role permissions:**
```bash
# List all roles
kubectl get roles --all-namespaces

# Check role bindings
kubectl get rolebindings --all-namespaces

# Check cluster roles
kubectl get clusterroles

# Check user permissions
kubectl auth can-i --list --as=system:serviceaccount:default:sa-name
```

**Common issues:**
- Excessive permissions
- Unused roles
- Unused service accounts

### Network Policies

**Check network policies:**
```bash
# List all network policies
kubectl get networkpolicies --all-namespaces

# Inspect a network policy
kubectl describe networkpolicy policy-name -n namespace
```

**Example network policy:**
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  - Egress
```

## Tooling

### Falco

**Runtime security monitoring:**
```bash
# Install Falco
helm repo add falcosecurity https://falcosecurity.github.io/charts
helm install falco falcosecurity/falco

# Check rules
falco -r /etc/falco/rules.d/
```

### Aqua Security

```bash
# Scan an image
aqua image scan nginx:latest

# Scan a Kubernetes cluster
aqua k8s scan
```

### Snyk

```bash
# Scan a Dockerfile
snyk test --docker nginx:latest

# Scan Kubernetes manifests
snyk iac test k8s/
```

## Testing Checklist

### Image Security
- [ ] Scan base images for vulnerabilities
- [ ] Scan dependency packages for vulnerabilities
- [ ] Review Dockerfile configuration
- [ ] Check for sensitive data exposure

### Runtime Security
- [ ] Check container privileges
- [ ] Check resource limits
- [ ] Check network isolation
- [ ] Check filesystem mounts

### Orchestration Security
- [ ] Check Kubernetes configuration
- [ ] Check RBAC configuration
- [ ] Check network policies
- [ ] Check Pod security policies

## Common Security Issues

### 1. Image Vulnerabilities

**Problem:**
- Base images contain vulnerabilities
- Dependency packages contain vulnerabilities
- Missing timely updates

**Fix:**
- Scan images regularly
- Keep base images up to date
- Use minimal images

### 2. Excessive Privileges

**Problem:**
- Containers running as root
- Privileged mode
- Sensitive directories mounted

**Fix:**
- Run as a non-root user
- Disable privileged mode
- Restrict filesystem access

### 3. Misconfiguration

**Problem:**
- Insecure default configuration
- Missing network policies
- Incorrect RBAC configuration

**Fix:**
- Follow security best practices
- Enforce network policies
- Configure RBAC correctly

### 4. Sensitive Data Exposure

**Problem:**
- Secrets baked into images
- Environment variable exposure
- Configuration file leakage

**Fix:**
- Use secrets management
- Avoid hardcoding
- Use Secret objects

## Best Practices

### 1. Image Security

- Use official base images
- Update images regularly
- Scan images for vulnerabilities
- Keep image size minimal

### 2. Runtime Security

- Run as a non-root user
- Restrict container privileges
- Enforce resource limits
- Enable security contexts

### 3. Orchestration Security

- Configure network policies
- Enforce RBAC
- Use Pod security policies
- Enable audit logging

## Notes

- Test only in authorized environments
- Avoid impacting production
- Be aware of differences between container platforms
- Run security scans on a regular schedule
