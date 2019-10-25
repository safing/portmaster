

Intel:
- First ever request: use first resolver as selected
- If resolver fails:
    - stop all requesting
    - get network status
        - if failed: do nothing, return offline error
    - check list front to back, use first resolver that resolves one.one.one.one correctly

NetEnv:
- check for intercepted HTTP Request requests
- if fails on:
    - connection establishment: OFFLINE
    - 
- check for intercepted HTTPS Request requests


- check for intercepted DNS requests
