# Teams Hook

A hook program that forwards incoming Microsoft Teams websocket messages to all subscribed clients.
The source of those notifications is the [Teams Hook Client](https://github.com/COM8/teams-client-hook) Microsoft Edge extension that forwards all incoming websocket messages to this WebHook.

## Development
## Self-Signed Certificate

To create a temporary self-signed certificate run the following command.

```bash
openssl req -new -x509 -nodes -newkey ec:<(openssl ecparam -name secp384r1) -keyout cert.key -out cert.crt -days 3650
```