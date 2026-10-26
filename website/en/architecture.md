# Architecture and transport

Kylok adds device-management capabilities to applications you already own. The Go SDK runs inside your server; the Android library runs inside your device app. You keep control of authorization, server lifecycle, Android application ID, signing and the experience your users see. The examples show one way to connect the two sides.

| Component | Responsibility |
| --- | --- |
| Go server SDK | Create enrollment codes, manage devices, assign apps and set policies. |
| Built-in storage | Keep device records, policies and verified APKs in SQLite and the filesystem. |
| Android library | Join a managed fleet, receive assignments, install apps and report device status. |
| Your applications | Handle business authorization, server lifecycle, app identity, signing and user interface. |

## Server addresses and transport

Use an IP address or domain name with HTTP or HTTPS. Kylok follows the scheme you configure and never downgrades HTTPS automatically. HTTPS verifies the certificate and server name; HTTP does not encrypt enrollment tokens or device credentials in transit. You can host the API and the initial Android APK at different addresses.

The Android SDK communicates over HTTP/2. If you serve HTTP, your Go listener must support h2c; HTTPS listeners must support HTTP/2. Before QR provisioning downloads the Android app, Android Setup Wizard must trust the certificate used by the APK server. Trust settings inside the installed app cannot change that earlier check.

## Durable identities and device changes

Each enrollment QR contains a single-use token tied to a management scope. If setup is interrupted, the device can retry the same enrollment and recover the same identity. Revoked devices cannot reconnect with their old credentials.

Kylok keeps requested app and policy state separate from what Android reports as installed or applied. A download, installer callback or policy command is not treated as success on its own: Kylok checks the installed package and version, and reports policy outcomes per rule. Saved work can resume after a network or process interruption.

If you provide your own storage, it must preserve device isolation and durable operations. Status and event callbacks are optional notifications; use your own application storage for records that must become part of a durable business or audit log.
