<div class="hero"><p class="eyebrow">OPEN SOURCE · GO + KOTLIN · LGPL-3.0-ONLY</p><h1>Bring Android device management<br>into your product.</h1><p>Give your team a ready foundation for device enrollment, app delivery and policy control. Kylok pairs a Go server SDK with an Android Device Policy Controller, running on infrastructure you own, so you can focus on the experience your users need.</p><div class="hero-actions"><a class="button" href="install.html">Start building with Kylok →</a><a class="button secondary" href="examples.html">Explore the examples</a></div><div class="network"><span>Your Go service</span><b>HTTP / HTTPS · your network</b><span>Your managed Android devices</span></div></div>

## The foundation for your device experience

<div class="features"><article><h3>Enroll devices with ease</h3><p>Create scoped, single-use QR enrollment for the IP or domain your service uses, over HTTP or HTTPS.</p></article><article><h3>Put the right apps on every device</h3><p>Assign signed APKs, resume interrupted downloads and verify the installed package, version and signing identity.</p></article><article><h3>Know what policies actually applied</h3><p>Manage required apps, Lock Task, runtime permissions, selected restrictions and typed app configuration with per-rule status.</p></article><article><h3>Keep control in your hands</h3><p>Integrate Go and Android APIs with durable local storage or optional PostgreSQL. Your host owns authorization, identity, interface and service lifecycle.</p></article><article><h3>Manage fleets with confidence</h3><p>Group devices, run bounded bulk assignments, schedule app deployment and track reboot or secure-lock outcomes.</p></article><article><h3>Make network policy fit your needs</h3><p>Optionally manage Wi-Fi profiles and private certificate references with recovery checks, while keeping your host's network and trust decisions in your hands.</p></article></div>

## Build your experience on a proven foundation

Explore the reference web console for enrollment, app delivery and policy management. The Android dashboard gives you a starting point for device status and actions. Use the examples to move quickly, then shape the operator and business-app experience around your product.

[Run the examples](examples.html) · [Integrate the SDK](install.html) · [Browse the API reference](api.html)

> **Experimental.** Public packages are listed in [GitHub Releases](https://github.com/thienvu18/kylok/releases). Build paired SDKs from the same source revision. Device support depends on tested model, firmware and network; see [compatibility](compatibility.html).

## Growing into dedicated-device deployments

Current source implements persistent kiosk launch and recovery, on-device compliance inspection, WorkManager sync, OS-level policy enforcement, and silent APK installation. The roadmap focuses next on physical device qualification, verified Wi-Fi lockdown controls, safe app update deferral, optional local messaging and stable release gates. Browse the [roadmap](roadmap.html), [local messaging design](https://github.com/thienvu18/kylok/blob/main/docs/local-messaging.md) and [implementation plan](https://github.com/thienvu18/kylok/blob/main/docs/implementation-plan.md).
