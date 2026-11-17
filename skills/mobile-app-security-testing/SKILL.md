---
name: mobile-app-security-testing
description: Specialized techniques and methodology for mobile application security testing
version: 1.0.0
---

# Mobile Application Security Testing

## Overview

Mobile application security testing is a critical step in ensuring the security of mobile apps. This skill provides the methodology, tools, and best practices for mobile application security testing, covering both Android and iOS.

## Testing Scope

### 1. Application Security

**Checks:**
- Code obfuscation
- Anti-decompilation
- Anti-debugging
- Certificate pinning

### 2. Data Security

**Checks:**
- Data encryption
- Key management
- Sensitive data storage
- Data in transit

### 3. Authentication and Authorization

**Checks:**
- Authentication mechanisms
- Token management
- Biometric authentication
- Session management

### 4. Communication Security

**Checks:**
- TLS/SSL configuration
- Certificate validation
- API security
- Man-in-the-middle attack protection

## Android Security Testing

### Static Analysis

**Using APKTool:**
```bash
# Decompile the APK
apktool d app.apk

# Inspect AndroidManifest.xml
cat app/AndroidManifest.xml

# Inspect Smali code
find app/smali -name "*.smali"
```

**Using Jadx:**
```bash
# Decompile the APK
jadx -d output app.apk

# Inspect Java source
find output -name "*.java"
```

**Using MobSF:**
```bash
# Start MobSF
docker run -it -p 8000:8000 opensecurity/mobsf

# Upload the APK for analysis
# Visit http://localhost:8000
```

### Dynamic Analysis

**Using Frida:**
```javascript
// Hook a method
Java.perform(function() {
    var MainActivity = Java.use("com.example.MainActivity");
    MainActivity.onCreate.implementation = function(savedInstanceState) {
        console.log("[*] onCreate called");
        this.onCreate(savedInstanceState);
    };
});
```

**Using Objection:**
```bash
# Start Objection
objection -g com.example.app explore

# Hook a method
android hooking watch class_method com.example.MainActivity.onCreate
```

**Using Burp Suite:**
```bash
# Configure the proxy
# Point the Android proxy settings at Burp Suite
# Install the Burp certificate
```

### Common Vulnerabilities

**Hardcoded secrets:**
```java
// Insecure code
String apiKey = "1234567890abcdef";
String password = "admin123";
```

**Insecure storage:**
```java
// Storing sensitive data in SharedPreferences
SharedPreferences prefs = getSharedPreferences("data", MODE_WORLD_READABLE);
prefs.edit().putString("password", password).apply();
```

**Certificate validation bypass:**
```java
// Skipping certificate validation
TrustManager[] trustAllCerts = new TrustManager[] {
    new X509TrustManager() {
        public X509Certificate[] getAcceptedIssuers() { return null; }
        public void checkClientTrusted(X509Certificate[] certs, String authType) { }
        public void checkServerTrusted(X509Certificate[] certs, String authType) { }
    }
};
```

## iOS Security Testing

### Static Analysis

**Using class-dump:**
```bash
# Dump header files
class-dump app.ipa

# Inspect the headers
find app -name "*.h"
```

**Using Hopper:**
```bash
# Disassemble with Hopper
# Open the app binary
# Analyze the assembly
```

**Using otool:**
```bash
# Inspect Mach-O metadata
otool -L app

# Inspect strings
strings app | grep -i "password\|key\|secret"
```

### Dynamic Analysis

**Using Frida:**
```javascript
// Hook an Objective-C method
var className = ObjC.classes.ViewController;
var method = className['- login:password:'];
Interceptor.attach(method.implementation, {
    onEnter: function(args) {
        console.log("[*] Login called");
        console.log("Username: " + ObjC.Object(args[2]).toString());
        console.log("Password: " + ObjC.Object(args[3]).toString());
    }
});
```

**Using Cycript:**
```bash
# Attach to the process
cycript -p app

# Run commands
[UIApplication sharedApplication]
```

### Common Vulnerabilities

**Hardcoded secrets:**
```objective-c
// Insecure code
NSString *apiKey = @"1234567890abcdef";
NSString *password = @"admin123";
```

**Insecure storage:**
```objective-c
// Improper Keychain usage
NSUserDefaults *defaults = [NSUserDefaults standardUserDefaults];
[defaults setObject:password forKey:@"password"];
```

**Certificate validation bypass:**
```objective-c
// Skipping certificate validation
- (void)connection:(NSURLConnection *)connection 
didReceiveAuthenticationChallenge:(NSURLAuthenticationChallenge *)challenge {
    [challenge.sender useCredential:[NSURLCredential credentialForTrust:challenge.protectionSpace.serverTrust] 
          forAuthenticationChallenge:challenge];
}
```

## Tool Usage

### MobSF

```bash
# Start MobSF
docker run -it -p 8000:8000 opensecurity/mobsf

# Upload the app for analysis
# Supports both Android and iOS
```

### Frida

```bash
# Install Frida
pip install frida-tools

# Run a script
frida -U -f com.example.app -l script.js
```

### Objection

```bash
# Install Objection
pip install objection

# Start Objection
objection -g com.example.app explore
```

### Burp Suite

**Proxy configuration:**
1. Configure the Burp Suite listener
2. Set the proxy on the mobile device
3. Install the Burp certificate
4. Intercept and analyze traffic

## Testing Checklist

### Application Security
- [ ] Code obfuscation check
- [ ] Anti-decompilation
- [ ] Anti-debugging
- [ ] Certificate pinning

### Data Security
- [ ] Data encryption check
- [ ] Key management
- [ ] Sensitive data storage
- [ ] Data in transit

### Authentication and Authorization
- [ ] Authentication mechanism testing
- [ ] Token management
- [ ] Session management
- [ ] Biometric authentication

### Communication Security
- [ ] TLS/SSL configuration
- [ ] Certificate validation
- [ ] API security testing
- [ ] Man-in-the-middle attack protection

## Common Security Issues

### 1. Hardcoded Secrets

**Issue:**
- Hardcoded API keys
- Hardcoded passwords
- Hardcoded encryption keys

**Remediation:**
- Use a key management service
- Use environment variables
- Use secure storage

### 2. Insecure Storage

**Issue:**
- Sensitive data stored in plaintext
- Insecure storage mechanisms
- Unencrypted data

**Remediation:**
- Use encrypted storage
- Use Keychain/Keystore
- Encrypt data

### 3. Certificate Validation Bypass

**Issue:**
- SSL certificates not validated
- Self-signed certificates accepted
- Certificate pinning not implemented

**Remediation:**
- Implement certificate pinning
- Validate the certificate chain
- Use the system trust store

### 4. Debug Information Leakage

**Issue:**
- Logs containing sensitive data
- Error message disclosure
- Debug mode left enabled

**Remediation:**
- Remove debug code
- Restrict log output
- Disable debugging in production

## Best Practices

### 1. Code Security

- Apply code obfuscation
- Disable debug features
- Add anti-debugging protection
- Use certificate pinning

### 2. Data Security

- Encrypt sensitive data
- Use secure storage
- Enforce key management
- Restrict data access

### 3. Communication Security

- Use TLS/SSL
- Implement certificate pinning
- Validate server certificates
- Use secure APIs

### 4. Authentication Security

- Enforce strong authentication
- Handle tokens securely
- Implement session management
- Use biometric authentication

## Notes

- Test only in authorized environments
- Comply with applicable laws and regulations
- Account for platform-specific differences
- Protect user privacy
