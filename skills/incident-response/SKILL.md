---
name: incident-response
description: Specialized skill and methodology for security incident response
version: 1.0.0
---

# Security Incident Response

## Overview

Security incident response is the critical process for handling security incidents. This skill covers incident response methods, tools, and best practices.

## Response Process

### 1. Preparation Phase

**Preparation tasks:**
- Form a response team
- Define a response plan
- Prepare tools and resources
- Establish communication channels

### 2. Identification Phase

**Identifying incidents:**
- Monitoring alerts
- Anomaly detection
- Log analysis
- User reports

### 3. Containment Phase

**Containment measures:**
- Isolate affected systems
- Disable accounts
- Block network connections
- Back up evidence

### 4. Eradication Phase

**Removing threats:**
- Remove malware
- Fix vulnerabilities
- Reset credentials
- Clean up backdoors

### 5. Recovery Phase

**Restoring systems:**
- Restore backups
- Verify system integrity
- Monitor systems
- Restore services gradually

### 6. Lessons-Learned Phase

**Reviewing the incident:**
- Incident report
- Lessons learned
- Improvement actions
- Process updates

## Tooling

### Log Analysis

**Using Splunk:**
```bash
# Search logs
index=security event_type="failed_login"

# Statistical analysis
index=security | stats count by src_ip

# Time series analysis
index=security | timechart count by event_type
```

**Using ELK:**
```bash
# Elasticsearch query
GET /logs/_search
{
  "query": {
    "match": {
      "event_type": "malware"
    }
  }
}
```

### Forensics Tools

**Using Volatility:**
```bash
# Analyze a memory image
volatility -f memory.dump imageinfo

# List processes
volatility -f memory.dump --profile=Win7SP1x64 pslist

# Dump process memory
volatility -f memory.dump --profile=Win7SP1x64 memdump -p 1234 -D output/
```

**Using Autopsy:**
```bash
# Start Autopsy
# Create a case
# Add evidence
# Analyze the data
```

### Network Analysis

**Using Wireshark:**
```bash
# Capture traffic
wireshark -i eth0

# Analyze a PCAP file
wireshark -r capture.pcap

# Filter traffic
# Display filter: ip.addr == 192.168.1.100
# Capture filter: host 192.168.1.100
```

**Using tcpdump:**
```bash
# Capture traffic
tcpdump -i eth0 -w capture.pcap

# Analyze the capture
tcpdump -r capture.pcap -A
```

## Incident Types

### Malware

**Response steps:**
1. Isolate affected systems
2. Collect samples
3. Analyze the malware
4. Remove the threat
5. Fix vulnerabilities

**Tools:**
- VirusTotal
- Cuckoo Sandbox
- YARA rules

### Data Breach

**Response steps:**
1. Confirm the breach scope
2. Contain the breach
3. Assess the impact
4. Notify affected parties
5. Fix vulnerabilities

**Checklist:**
- Volume of leaked data
- Affected users
- Leakage channels
- Data sensitivity

### Denial of Service

**Response steps:**
1. Identify the attack type
2. Enable protective measures
3. Filter malicious traffic
4. Monitor system status
5. Restore normal service

**Protective measures:**
- DDoS protection services
- Traffic scrubbing
- Rate limiting
- CDN protection

### Unauthorized Access

**Response steps:**
1. Disable affected accounts
2. Reset credentials
3. Review access logs
4. Assess data access
5. Fix vulnerabilities

**Checklist:**
- Access times
- Data accessed
- Access sources
- Data modifications

## Response Checklist

### Preparation Phase
- [ ] Form a response team
- [ ] Define a response plan
- [ ] Prepare tools
- [ ] Establish communication channels

### Identification Phase
- [ ] Confirm the incident
- [ ] Collect information
- [ ] Assess the impact
- [ ] Document the timeline

### Containment Phase
- [ ] Isolate systems
- [ ] Disable accounts
- [ ] Block connections
- [ ] Back up evidence

### Eradication Phase
- [ ] Remove threats
- [ ] Fix vulnerabilities
- [ ] Reset credentials
- [ ] Verify eradication

### Recovery Phase
- [ ] Restore systems
- [ ] Verify integrity
- [ ] Monitor systems
- [ ] Restore services

### Lessons-Learned Phase
- [ ] Write the report
- [ ] Capture lessons learned
- [ ] Improvement actions
- [ ] Process updates

## Best Practices

### 1. Prepare

- Form a response team
- Define a response plan
- Run regular drills
- Prepare tools

### 2. Respond

- Respond quickly
- Handle incidents systematically
- Log every action
- Preserve evidence

### 3. Communicate

- Internal communication
- External notifications
- Status updates
- Post-incident reports

### 4. Improve

- Incident analysis
- Process improvements
- Tool updates
- Training improvements

## Notes

- Respond quickly
- Preserve evidence
- Log all actions
- Comply with laws and regulations
