---
name: secure-code-review
description: Professional skill and methodology for security code review
version: 1.0.0
---

# Security Code Review

## Overview

Security code review is a key method for identifying security vulnerabilities in code. This skill provides the methodology, tools, and best practices for security code review.

## Review Scope

### 1. Input Validation

**Checks:**
- User input validation
- Parameter validation
- Data sanitization
- Boundary checks

### 2. Output Encoding

**Checks:**
- XSS protection
- Output encoding
- Content Security Policy
- Response header configuration

### 3. Authentication & Authorization

**Checks:**
- Authentication mechanisms
- Session management
- Access control
- Password handling

### 4. Cryptography & Keys

**Checks:**
- Data encryption
- Key management
- Hash algorithms
- Random number generation

## Review Methods

### 1. Static Analysis

**Using SAST tools:**
```bash
# SonarQube
sonar-scanner

# Checkmarx
# Use the web UI

# Fortify
sourceanalyzer -b project build.sh
sourceanalyzer -b project -scan

# Semgrep
semgrep --config=auto .
```

### 2. Manual Review

**Review checklist:**
- [ ] Input validation
- [ ] Output encoding
- [ ] SQL injection
- [ ] XSS vulnerabilities
- [ ] Authentication & authorization
- [ ] Cryptography usage
- [ ] Error handling
- [ ] Logging

### 3. Code Pattern Recognition

**Dangerous functions:**
```python
# Dangerous Python functions
eval()
exec()
pickle.loads()
os.system()
subprocess.call()
```

```java
// Dangerous Java functions
Runtime.exec()
ProcessBuilder()
Class.forName()
```

```php
// Dangerous PHP functions
eval()
exec()
system()
passthru()
```

## Common Vulnerability Patterns

### SQL Injection

**Vulnerable code:**
```java
String query = "SELECT * FROM users WHERE id = " + userId;
Statement stmt = connection.createStatement();
ResultSet rs = stmt.executeQuery(query);
```

**Secure code:**
```java
String query = "SELECT * FROM users WHERE id = ?";
PreparedStatement stmt = connection.prepareStatement(query);
stmt.setInt(1, userId);
ResultSet rs = stmt.executeQuery();
```

### XSS Vulnerabilities

**Vulnerable code:**
```javascript
document.innerHTML = userInput;
element.innerHTML = "<div>" + userInput + "</div>";
```

**Secure code:**
```javascript
element.textContent = userInput;
element.setAttribute("data-value", userInput);
// Or use an encoding library
element.innerHTML = escapeHtml(userInput);
```

### Command Injection

**Vulnerable code:**
```python
import os
os.system("ping " + user_input)
```

**Secure code:**
```python
import subprocess
subprocess.run(["ping", "-c", "1", validated_input])
```

### Path Traversal

**Vulnerable code:**
```java
String filePath = "/uploads/" + fileName;
File file = new File(filePath);
```

**Secure code:**
```java
String basePath = "/uploads/";
String fileName = Paths.get(fileName).getFileName().toString();
String filePath = basePath + fileName;
File file = new File(filePath);
if (!file.getCanonicalPath().startsWith(basePath)) {
    throw new SecurityException("Invalid path");
}
```

### Hardcoded Credentials

**Vulnerable code:**
```java
String apiKey = "1234567890abcdef";
String password = "admin123";
```

**Secure code:**
```java
String apiKey = System.getenv("API_KEY");
String password = keyStore.getPassword("db_password");
```

## Tool Usage

### SonarQube

```bash
# Start SonarQube
docker run -d -p 9000:9000 sonarqube

# Run a scan
sonar-scanner \
  -Dsonar.projectKey=myproject \
  -Dsonar.sources=. \
  -Dsonar.host.url=http://localhost:9000
```

### Semgrep

```bash
# Install
pip install semgrep

# Run a scan
semgrep --config=auto .

# Use rules
semgrep --config=p/security-audit .
```

### CodeQL

```bash
# Create a database
codeql database create database --language=java --source-root=.

# Run queries
codeql database analyze database security-and-quality.qls --format=sarif-latest
```

## Review Checklist

### Input Validation
- [ ] All user input is validated
- [ ] Allowlist-based validation is used
- [ ] Data types and ranges are validated
- [ ] Special characters are handled

### Output Encoding
- [ ] HTML output encoding
- [ ] URL encoding
- [ ] JavaScript encoding
- [ ] SQL parameterization

### Authentication & Authorization
- [ ] Strong password policy
- [ ] Secure session management
- [ ] Authorization checks
- [ ] Multi-factor authentication

### Cryptography
- [ ] Strong encryption algorithms
- [ ] Secure key storage
- [ ] Encryption in transit
- [ ] Encryption at rest

### Error Handling
- [ ] No sensitive information disclosure
- [ ] Consistent error responses
- [ ] Errors are logged
- [ ] Exception handling

## Best Practices

### 1. Secure Coding Standards

- Follow the OWASP Top 10
- Apply secure coding guidelines
- Code review process
- Security training

### 2. Automated Tooling

- Integrate SAST tools
- CI/CD security gates
- Automated scanning
- Result analysis

### 3. Code Review Process

- Peer review
- Security expert review
- Periodic reviews
- Issue tracking

## Notes

- Combine tooling with manual review
- Pay attention to business logic vulnerabilities
- Keep tool rules up to date
- Build a secure coding culture
