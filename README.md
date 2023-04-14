# Teams Hook

A hook program translating MS Teams (Graph) notification webhooks to a websocket session.


## Self Signed Certificate

```bash
openssl req -new -x509 -nodes -newkey ec:<(openssl ecparam -name secp384r1) -keyout cert.key -out cert.crt -days 3650
```