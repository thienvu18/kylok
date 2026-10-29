// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.app.admin.DevicePolicyManager
import android.app.admin.TestDevicePolicyManager
import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.net.TestNetwork
import android.os.Build
import android.os.PersistableBundle
import androidx.work.ListenableWorker
import androidx.work.WorkerParameters
import io.github.thienvu18.kylok.proto.v1.*
import io.grpc.NameResolver
import io.grpc.Status
import io.grpc.StatusRuntimeException
import org.json.JSONArray
import org.json.JSONObject
import org.junit.After
import org.junit.Assert.*
import org.junit.Before
import org.junit.Test
import java.io.IOException
import java.net.URI
import java.util.concurrent.CountDownLatch
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicReference
import javax.net.ssl.SSLContext

/** Unit tests covering Kylok facade, EnrollmentWorker lifecycle, and AgentClient networking. */
class KylokAndWorkerTest {
    private lateinit var context: TestContext
    private lateinit var fakeDb: TestAgentDatabase
    private lateinit var dpm: TestDevicePolicyManager

    @Before
    fun setUp() {
        TestAndroidKeyStoreProvider.install()
        TestAndroidKeyStoreProvider.reset()
        context = TestContext()
        fakeDb = TestAgentDatabase()

        Kylok.executor.shutdownNow()
        Kylok.executor = java.util.concurrent.Executors.newSingleThreadExecutor()

        val configField = Kylok::class.java.getDeclaredField("configuration").apply { isAccessible = true }
        val hostConfig = configField.get(Kylok)
        val frozenField = HostConfiguration::class.java.getDeclaredField("frozen").apply { isAccessible = true }
        frozenField.set(hostConfig, false)
        val optionsField = HostConfiguration::class.java.getDeclaredField("options").apply { isAccessible = true }
        optionsField.set(hostConfig, HostOptions())

        val dbField = AgentStorage::class.java.getDeclaredField("database")
        dbField.isAccessible = true
        dbField.set(AgentStorage, fakeDb)

        dpm = TestDevicePolicyManager()
        dpm.owner = true
        context.registerService(DevicePolicyManager::class.java, dpm)

        val wifiManager = android.net.wifi.TestWifiManager()
        context.registerService(android.net.wifi.WifiManager::class.java, wifiManager)

        val connectivityManager = android.net.TestConnectivityManager()
        context.registerService(android.net.ConnectivityManager::class.java, connectivityManager)

        val activityManager = android.app.TestActivityManager()
        context.registerService(android.app.ActivityManager::class.java, activityManager)

        val keyguardManager = android.app.TestKeyguardManager()
        context.registerService(android.app.KeyguardManager::class.java, keyguardManager)

        val userManager = android.os.TestUserManager()
        context.registerService(android.os.UserManager::class.java, userManager)

        val storageManager = android.os.storage.TestStorageManager()
        context.registerService(android.os.storage.StorageManager::class.java, storageManager)

        context.defaultPackageManager.hasSystemFeatureHandler = java.util.function.BiFunction { name, _ ->
            name == PackageManager.FEATURE_WIFI
        }

        androidx.work.WorkManager.initialize(context, androidx.work.Configuration.Builder().build())
    }

    @After
    fun tearDown() {
        android.os.Build.VERSION.SDK_INT = 0
        AgentClient.clientFactory = null
        Kylok.executor.shutdownNow()
        Kylok.executor = java.util.concurrent.Executors.newSingleThreadExecutor()
        val dbField = AgentStorage::class.java.getDeclaredField("database")
        dbField.isAccessible = true
        dbField.set(AgentStorage, null)
        TestAndroidKeyStoreProvider.reset()
    }

    private fun createWorkerParameters(): WorkerParameters {
        val unsafeField = sun.misc.Unsafe::class.java.getDeclaredField("theUnsafe").apply { isAccessible = true }
        val unsafe = unsafeField.get(null) as sun.misc.Unsafe
        return unsafe.allocateInstance(WorkerParameters::class.java) as WorkerParameters
    }

    @Test
    fun testKylokVersionAndOptions() {
        assertEquals(BuildConfig.SDK_VERSION, Kylok.sdkVersion)
        assertEquals(BuildConfig.PROTOCOL_VERSION, Kylok.protocolVersion)

        // configure() with valid options
        val opts = HostOptions(syncIntervalMinutes = 30, retryBackoffSeconds = 60, wakeJitterSeconds = 10)
        Kylok.configure(context, opts)

        // Equal options repeated is idempotent
        Kylok.configure(context, opts)

        val frozen = Kylok.initialize()
        assertEquals(30, frozen.syncIntervalMinutes)
        assertEquals(60, frozen.retryBackoffSeconds)
        assertEquals(10, frozen.wakeJitterSeconds)

        // Different options after freeze throws IllegalStateException
        val diffOpts = HostOptions(syncIntervalMinutes = 60)
        assertThrows(IllegalStateException::class.java) {
            Kylok.configure(context, diffOpts)
        }
    }

    @Test
    fun testKylokOwnerAndLan() {
        dpm.owner = true
        assertTrue(Kylok.owner(context))

        dpm.owner = false
        assertFalse(Kylok.owner(context))

        // lanGranted
        assertTrue(Kylok.lanGranted(context))
    }

    @Test
    fun testKylokScheduleAndRetry() {
        dpm.owner = false
        // Non-owner early return without throwing
        Kylok.schedule(context)

        dpm.owner = true
        Kylok.schedule(context)
        Kylok.retry(context)
    }

    @Test
    fun testKylokSnapshotAndStatus() {
        val latch = CountDownLatch(1)
        val statusRef = AtomicReference<AgentStatus>()
        Kylok.snapshot(context) { status ->
            statusRef.set(status)
            latch.countDown()
        }
        assertTrue(latch.await(3, TimeUnit.SECONDS))
        assertNotNull(statusRef.get())

        val statusLatch = CountDownLatch(1)
        val textRef = AtomicReference<String>()
        Kylok.status(context) { text ->
            textRef.set(text)
            statusLatch.countDown()
        }
        assertTrue(statusLatch.await(3, TimeUnit.SECONDS))
        assertTrue(textRef.get().contains("Device Owner:"))

        // Status with policy report (with reason and without reason)
        val report = PolicyReport.newBuilder().setRevision(10)
            .addResults(PolicyResult.newBuilder().setRuleId("test_rule").setState(ComplianceState.COMPLIANCE_STATE_APPLIED).setReason("ok").build())
            .addResults(PolicyResult.newBuilder().setRuleId("rule_no_reason").setState(ComplianceState.COMPLIANCE_STATE_APPLIED).setReason("").build())
            .build()
        AgentStorage.update(context) {
            it.put("policy_report", encode(report))
        }
        val statusLatch2 = CountDownLatch(1)
        val textRef2 = AtomicReference<String>()
        Kylok.status(context) { text ->
            textRef2.set(text)
            statusLatch2.countDown()
        }
        assertTrue(statusLatch2.await(3, TimeUnit.SECONDS))
        assertTrue(textRef2.get().contains("test_rule: APPLIED (ok)"))
        assertTrue(textRef2.get().contains("rule_no_reason: APPLIED\n") || textRef2.get().endsWith("rule_no_reason: APPLIED"))

        // Corrupt storage to test read error fallbacks
        fakeDb.recordRef.set(io.github.thienvu18.kylok.dpc.storage.AgentRecord().apply {
            id = 1
            sealed = byteArrayOf(1, 2, 3)
        })
        val errorLatch = CountDownLatch(1)
        val errorStatusRef = AtomicReference<AgentStatus>()
        Kylok.snapshot(context) { status ->
            errorStatusRef.set(status)
            errorLatch.countDown()
        }
        assertTrue(errorLatch.await(3, TimeUnit.SECONDS))
        assertEquals("ERROR", errorStatusRef.get().lifecycle)
        assertTrue(errorStatusRef.get().lastResult.contains("Cannot read protected state"))

        val errorTextLatch = CountDownLatch(1)
        val errorTextRef = AtomicReference<String>()
        Kylok.status(context) { text ->
            errorTextRef.set(text)
            errorTextLatch.countDown()
        }
        assertTrue(errorTextLatch.await(3, TimeUnit.SECONDS))
        assertTrue(errorTextRef.get().contains("Cannot read protected agent state"))
        fakeDb.recordRef.set(null)
    }

    @Test
    fun testKylokConfirmPendingInstall() {
        // No operations
        Kylok.confirmPendingInstall(context)
        Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
        assertEquals(0, context.startedActivities.size)

        // Superseded or terminal operation
        AgentStorage.update(context) { state ->
            val ops = JSONObject()
            ops.put("op-1", JSONObject().put("superseded", true).put("confirmation", "intent:#Intent;action=TEST;end"))
            ops.put("op-2", JSONObject().put("terminal", true).put("confirmation", "intent:#Intent;action=TEST;end"))
            state.put("operations", ops)
        }
        Kylok.confirmPendingInstall(context)
        Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
        assertEquals(0, context.startedActivities.size)

        // Matching active confirmation
        AgentStorage.update(context) { state ->
            val ops = JSONObject()
            ops.put("op-3", JSONObject().put("confirmation", "intent:#Intent;action=io.github.thienvu18.CONFIRM;end"))
            state.put("operations", ops)
        }
        Kylok.confirmPendingInstall(context)
        Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
        assertEquals(1, context.startedActivities.size)
        assertEquals("io.github.thienvu18.CONFIRM", context.startedActivities[0].action)
    }

    @Test
    fun testKylokObserveAndNotifyStatus() {
        val observed = mutableListOf<AgentStatus>()
        val listener = StatusListener { observed.add(it) }
        val reg = Kylok.observe(listener)

        Kylok.notifyStatus(context)
        Thread.sleep(100)
        assertTrue(observed.isNotEmpty())

        // Fault tolerant listener
        val failingReg = Kylok.observe { throw RuntimeException("Faulty listener") }
        Kylok.notifyStatus(context)
        Thread.sleep(100)
        failingReg.close()

        reg.close()
        val count = observed.size
        Kylok.notifyStatus(context)
        Thread.sleep(100)
        assertEquals(count, observed.size)
    }

    @Test
    fun testKylokCaptureAndConfigure() {
        // configure with HostOptions
        val hostOpts = HostOptions(syncIntervalMinutes = 20, wakeJitterSeconds = 5)
        Kylok.configure(context, hostOpts)

        // capture: first call succeeds
        val extras = android.os.PersistableBundle().apply {
            putString("kylok.server", "http://192.168.1.100")
            putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP1")
        }
        val intent = Intent().apply {
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, extras)
        }
        val latch1 = CountDownLatch(1)
        var success1 = false
        Kylok.capture(context, intent, "CALLBACK_1") {
            success1 = it
            latch1.countDown()
        }
        assertTrue(latch1.await(3, TimeUnit.SECONDS))
        assertTrue(success1)

        // capture: idempotent second call with same server and token
        val latch2 = CountDownLatch(1)
        var success2 = false
        Kylok.capture(context, intent, "CALLBACK_2") {
            success2 = it
            latch2.countDown()
        }
        assertTrue(latch2.await(3, TimeUnit.SECONDS))
        assertTrue(success2)

        // capture: mismatched server fails
        val badServerExtras = android.os.PersistableBundle().apply {
            putString("kylok.server", "http://192.168.1.200")
            putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP1")
        }
        val badServerIntent = Intent().apply {
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, badServerExtras)
        }
        val latch3 = CountDownLatch(1)
        var success3 = true
        Kylok.capture(context, badServerIntent, "CALLBACK_3") {
            success3 = it
            latch3.countDown()
        }
        assertTrue(latch3.await(3, TimeUnit.SECONDS))
        assertFalse(success3)

        // capture: mismatched token fails
        val badTokenExtras = android.os.PersistableBundle().apply {
            putString("kylok.server", "http://192.168.1.100")
            putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPX")
        }
        val badTokenIntent = Intent().apply {
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, badTokenExtras)
        }
        val latch4 = CountDownLatch(1)
        var success4 = true
        Kylok.capture(context, badTokenIntent, "CALLBACK_4") {
            success4 = it
            latch4.countDown()
        }
        assertTrue(latch4.await(3, TimeUnit.SECONDS))
        assertFalse(success4)

        // configure: bad context with null applicationContext throws IllegalArgumentException
        val badCtx = TestContext().apply { forceNullAppContext = true }
        assertThrows(IllegalArgumentException::class.java) {
            Kylok.configure(badCtx, HostOptions())
        }

        // capture: server with path fails validation
        val badPathExtras = android.os.PersistableBundle().apply {
            putString("kylok.server", "http://192.168.1.100/invalid_path")
            putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP1")
        }
        val badPathIntent = Intent().apply {
            putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, badPathExtras)
        }
        val latchPath = CountDownLatch(1)
        var successPath = true
        Kylok.capture(context, badPathIntent, "CALLBACK_PATH") {
            successPath = it
            latchPath.countDown()
        }
        assertTrue(latchPath.await(3, TimeUnit.SECONDS))
        assertFalse(successPath)
    }

    @Test
    fun testStatusValueAndDataClasses() {
        val opProto = Operation.newBuilder()
            .setId("op-xyz")
            .setRevision(1)
            .setArtifact(Artifact.newBuilder().setPackageName("com.pkg.test").setVersionCode(5).setSizeBytes(1024).build())
            .build()
        val cmdProto = Command.newBuilder()
            .setId("cmd-1")
            .setKind("REBOOT")
            .setDispatchToken("0123456789abcdef0123456789abcdef0123456789abcdef")
            .setDeadlineUnixMillis(System.currentTimeMillis() + 60000)
            .build()
        val cmdProto2 = cmdProto.toBuilder().setId("cmd-2").setKind("LOCK").build()
        val cmdProto3 = cmdProto.toBuilder().setId("cmd-3").setKind("REBOOT").build()

        val snap = SyncResponse.newBuilder()
            .setDevice(Device.newBuilder().setId("dev-1").setScopeId("scope-1").setLastSeenAtUnixMillis(123456789L).setInventory(
                Inventory.newBuilder().addApplications(InstalledApplication.newBuilder().setPackageName("com.pkg.test").setVersionCode(5).build()).build()
            ))
            .addDesiredApplications(DesiredApplication.newBuilder().setRevision(1).setArtifact(opProto.artifact))
            .addCompliance(ApplicationCompliance.newBuilder().setPackageName("com.pkg.test").setState(ComplianceState.COMPLIANCE_STATE_APPLIED).setReason("installed"))
            .setDesiredNetworkPolicy(DesiredNetworkPolicy.newBuilder().setRevision(5).setPolicy(NetworkPolicy.newBuilder().build()))
            .build()

        val policyReport = PolicyReport.newBuilder()
            .setSnapshotRevision(10)
            .addResults(PolicyResult.newBuilder().setRuleId("camera_rule").setState(ComplianceState.COMPLIANCE_STATE_APPLIED).setReason("applied").setObservedValue("true"))
            .build()

        val netReport = NetworkReport.newBuilder()
            .setRevision(5)
            .setState("APPLIED")
            .setReason("connected")
            .setActiveProfileId("prof-1")
            .setConnectivityVerified(true)
            .setCleanupComplete(true)
            .build()

        val state = JSONObject().apply {
            put("revision", 10L)
            put("sequence", 3L)
            put("lifecycle", "MANAGED")
            put("device_id", "dev-1")
            put("scope_id", "scope-1")
            put("server", "http://127.0.0.1:8080")
            put("result", "ok")
            put("snapshot", encode(snap))
            put("policy_report", encode(policyReport))
            put("network_report", encode(netReport))
            val ops = JSONObject().apply {
                put("op-1", JSONObject().put("superseded", true).put("operation", encode(opProto)))
                put("op-2", JSONObject().put("terminal", true).put("failure", "err").put("operation", encode(opProto)))
                put("op-3", JSONObject().put("terminal", true).put("operation", encode(opProto)))
                put("op-4", JSONObject().put("callback", "waiting").put("confirmation", "intent:#Intent;end").put("operation", encode(opProto)))
                put("op-5", JSONObject().put("stage", "downloading").put("bytes", 500L).put("operation", encode(opProto)))
            }
            put("operations", ops)
            val cmds = JSONObject().apply {
                put("cmd-1", JSONObject().put("stage", "armed").put("command", encode(cmdProto)))
                put("cmd-2", JSONObject().put("stage", "dispatched").put("command", encode(cmdProto2)))
                put("cmd-3", JSONObject().put("stage", "dispatched").put("command", encode(cmdProto3)))
            }
            put("commands", cmds)
            val cmdReports = JSONObject().apply {
                put("cmd-3", encode(CommandReport.newBuilder().setCommandId("cmd-3").setState("SUCCEEDED").setErrorCode("").build()))
            }
            put("command_reports", cmdReports)
        }

        val status = statusValue(state, owner = true, lan = true)
        assertEquals(1, status.policies.size)
        assertEquals("camera_rule", status.policies[0].ruleId)
        assertEquals("APPLIED", status.policies[0].state)
        assertEquals("applied", status.policies[0].reason)
        assertEquals("true", status.policies[0].observedValue)

        assertEquals(1, status.applications.size)
        assertEquals("com.pkg.test", status.applications[0].packageName)
        assertEquals(5L, status.applications[0].desiredVersion)
        assertEquals(5L, status.applications[0].observedVersion)
        assertEquals("APPLIED", status.applications[0].state)

        assertEquals(5, status.operations.size)
        val stages = status.operations.associate { it.id to it.stage }
        assertEquals("SUPERSEDED", stages["op-1"])
        assertEquals("FAILED", stages["op-2"])
        assertEquals("SUCCEEDED", stages["op-3"])
        assertEquals("WAITING_USER", stages["op-4"])
        assertEquals("DOWNLOADING", stages["op-5"])

        assertEquals(3, status.commands.size)
        val cmdStates = status.commands.associate { it.id to it.state }
        assertEquals("AWAITING_OBSERVATION", cmdStates["cmd-1"])
        assertEquals("DISPATCHED", cmdStates["cmd-2"])
        assertEquals("SUCCEEDED", cmdStates["cmd-3"])

        assertNotNull(status.network)
        assertEquals(5L, status.network!!.revision)
        assertEquals("APPLIED", status.network!!.state)
        assertTrue(status.network!!.connectivityVerified)
        assertTrue(status.network!!.cleanupComplete)

        // When policyReport snapshotRevision != state revision -> PENDING
        val stateMismatch = JSONObject(state.toString())
        stateMismatch.put("revision", 99L)
        val statusMismatch = statusValue(stateMismatch, owner = true, lan = true)
        assertEquals("PENDING", statusMismatch.policies[0].state)
        assertEquals("AWAITING_RECONCILIATION", statusMismatch.policies[0].reason)

        // Exercise data classes equals, hashCode, toString, copy
        val polStatus = status.policies[0]
        assertEquals(polStatus, polStatus.copy())
        assertEquals(polStatus.hashCode(), polStatus.copy().hashCode())
        assertTrue(polStatus.toString().contains("camera_rule"))

        val appStatus = status.applications[0]
        assertEquals(appStatus, appStatus.copy())
        assertEquals(appStatus.hashCode(), appStatus.copy().hashCode())
        assertTrue(appStatus.toString().contains("com.pkg.test"))

        val opStatus = status.operations[0]
        assertEquals(opStatus, opStatus.copy())
        assertEquals(opStatus.hashCode(), opStatus.copy().hashCode())
        assertTrue(opStatus.toString().contains("op-1"))

        val cStatus = status.commands[0]
        assertEquals(cStatus, cStatus.copy())
        assertEquals(cStatus.hashCode(), cStatus.copy().hashCode())
        assertTrue(cStatus.toString().contains("cmd-1"))

        val nStatus = status.network!!
        assertEquals(nStatus, nStatus.copy())
        assertEquals(nStatus.hashCode(), nStatus.copy().hashCode())
        assertTrue(nStatus.toString().contains("prof-1"))

        assertEquals(status, status.copy())
        assertEquals(status.hashCode(), status.copy().hashCode())
        assertTrue(status.toString().contains("MANAGED"))
    }

    @Test
    fun testAgentClientOriginHelpersAndConnect() {
        assertEquals("192.168.1.1", originAddressHost(URI("http://192.168.1.1:8080")))
        assertEquals("::1", originAddressHost(URI("http://[::1]:8080")))
        assertEquals("2001:db8::1", originAddressHost(URI("http://[2001:db8::1]:8080")))

        assertEquals("192.168.1.1:8080", originAuthority("192.168.1.1", 8080))
        assertEquals("[2001:db8::1]:8080", originAuthority("2001:db8::1", 8080))
        assertEquals("[2001:db8::1]:8080", originAuthority("[2001:db8::1]", 8080))

        // Connect valid schemes and configurations
        AgentClient.connect("http://127.0.0.1:8080").close()
        AgentClient.connect("https://127.0.0.1:8443").close()
        AgentClient.connect("https://example.com").close()

        val ssl = SSLContext.getDefault().socketFactory
        AgentClient.connect("https://example.com:443", trust = ssl).close()

        val net = TestNetwork()
        AgentClient.connect("http://127.0.0.1:8080", network = net).close()

        // Invalid origins
        assertThrows(IllegalArgumentException::class.java) { AgentClient.connect("ftp://example.com") }
        assertThrows(IllegalArgumentException::class.java) { AgentClient.connect("http:///path") }
        assertThrows(IllegalArgumentException::class.java) { AgentClient.connect("http://user:pass@example.com") }
        assertThrows(IllegalArgumentException::class.java) { AgentClient.connect("http://example.com/path") }
        assertThrows(IllegalArgumentException::class.java) { AgentClient.connect("http://example.com?query=1") }
        assertThrows(IllegalArgumentException::class.java) { AgentClient.connect("http://example.com#fragment") }
    }

    @Test
    fun testNetworkResolver() {
        val net = TestNetwork()
        val resolverFactory = NetworkResolver(net, "localhost", 8080)
        assertEquals("dns", resolverFactory.getDefaultScheme())

        val args = NameResolver.Args.newBuilder()
            .setDefaultPort(8080)
            .setProxyDetector { null }
            .setSynchronizationContext(io.grpc.SynchronizationContext { _, _ -> })
            .setServiceConfigParser(object : NameResolver.ServiceConfigParser() {
                override fun parseServiceConfig(rawConfig: Map<String, *>): NameResolver.ConfigOrError =
                    NameResolver.ConfigOrError.fromConfig(rawConfig)
            })
            .build()

        assertNull(resolverFactory.newNameResolver(URI("http://localhost"), args))

        val resolver = resolverFactory.newNameResolver(URI("dns:///localhost"), args)
        assertNotNull(resolver)
        assertEquals("localhost:8080", resolver!!.serviceAuthority)

        var resultReceived: NameResolver.ResolutionResult? = null
        var errorReceived: Status? = null
        val listener = object : NameResolver.Listener2() {
            override fun onResult(resolutionResult: NameResolver.ResolutionResult) {
                resultReceived = resolutionResult
            }
            override fun onError(error: Status) {
                errorReceived = error
            }
        }

        resolver.start(listener)
        Thread.sleep(100)
        assertNotNull(resultReceived)
        resolver.shutdown()

        // Test error branch
        net.allByName = { throw IOException("DNS resolution failed") }
        val resolver2 = resolverFactory.newNameResolver(URI("dns:///localhost"), args)!!
        resolver2.start(listener)
        Thread.sleep(100)
        assertNotNull(errorReceived)
        resolver2.shutdown()
    }

    @Test
    fun testAgentClientEnrollAndSyncStubExecution() {
        val mockChannel = object : io.grpc.ManagedChannel() {
            override fun shutdown(): io.grpc.ManagedChannel = this
            override fun isShutdown() = false
            override fun isTerminated() = false
            override fun shutdownNow(): io.grpc.ManagedChannel = this
            override fun awaitTermination(timeout: Long, unit: TimeUnit) = true
            override fun <RequestT, ResponseT> newCall(
                methodDescriptor: io.grpc.MethodDescriptor<RequestT, ResponseT>,
                callOptions: io.grpc.CallOptions
            ): io.grpc.ClientCall<RequestT, ResponseT> {
                return object : io.grpc.ClientCall<RequestT, ResponseT>() {
                    override fun start(responseListener: Listener<ResponseT>, headers: io.grpc.Metadata) {
                        responseListener.onClose(io.grpc.Status.UNAVAILABLE, io.grpc.Metadata())
                    }
                    override fun request(numMessages: Int) {}
                    override fun cancel(message: String?, cause: Throwable?) {}
                    override fun halfClose() {}
                    override fun sendMessage(message: RequestT) {}
                }
            }
            override fun authority() = "localhost"
        }

        val client = AgentClient(mockChannel)
        assertThrows(io.grpc.StatusRuntimeException::class.java) {
            client.enroll("token", "attempt", byteArrayOf(1, 2), Inventory.getDefaultInstance())
        }
        assertThrows(io.grpc.StatusRuntimeException::class.java) {
            client.sync(SyncRequest.getDefaultInstance(), "cred")
        }
        client.close()
    }

    @Test
    fun testEnrollmentWorkerDoWorkBranches() {
        val params = createWorkerParameters()
        val worker = EnrollmentWorker(context, params)

        // Branch 1: Not owner -> Result.failure()
        dpm.owner = false
        val r1 = worker.doWork()
        assertEquals(ListenableWorker.Result.failure(), r1)
        assertEquals("ERROR", AgentStorage.read(context).getString("lifecycle"))

        // Branch 2: Owner, but missing server -> Result.success()
        dpm.owner = true
        AgentStorage.update(context) { (it as JSONObject).remove("server") }
        val r2 = worker.doWork()
        assertEquals(ListenableWorker.Result.success(), r2)

        // Branch 3: Lifecycle REVOKED -> Result.failure()
        AgentStorage.update(context) { it.put("server", "http://127.0.0.1:8080").put("lifecycle", "REVOKED") }
        val r3 = worker.doWork()
        assertEquals(ListenableWorker.Result.failure(), r3)

        // Branch 4: Missing token and credential when server is present
        AgentStorage.update(context) {
            it.put("lifecycle", "PROVISIONING")
            (it as JSONObject).remove("credential")
            (it as JSONObject).remove("token")
        }
        val r4 = worker.doWork()
        assertEquals(ListenableWorker.Result.failure(), r4)

        // Branch 5: StatusRuntimeException classification
        // Server unreachable -> UNAVAILABLE -> returns Result.retry()
        AgentStorage.update(context) {
            it.put("server", "http://127.0.0.1:1")
                .put("token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP1")
                .put("attempt", "attempt-1")
            (it as JSONObject).remove("credential")
        }
        val r5 = worker.doWork()
        val s5 = AgentStorage.read(context)
        println("STATE5: lifecycle=${s5.optString("lifecycle")} result=${s5.optString("result")}")
        assertEquals(ListenableWorker.Result.retry(), r5)
        assertEquals("ENROLLING", AgentStorage.read(context).getString("lifecycle"))
        assertEquals("RPC UNAVAILABLE", AgentStorage.read(context).getString("result"))

        // Branch 6: LAN permission denied when SDK_INT >= 37
        try {
            android.os.Build.VERSION.SDK_INT = 37
            context.permissionGrants[Kylok.LAN_PERMISSION] = PackageManager.PERMISSION_DENIED
            context.defaultPackageManager.permissionGrants[Kylok.LAN_PERMISSION] = PackageManager.PERMISSION_DENIED
            AgentStorage.update(context) { it.put("credential", "cred-123") }
            val r6 = worker.doWork()
            assertEquals(ListenableWorker.Result.failure(), r6)
            assertEquals("ERROR", AgentStorage.read(context).getString("lifecycle"))
        } finally {
            android.os.Build.VERSION.SDK_INT = 0
            context.permissionGrants.remove(Kylok.LAN_PERMISSION)
            context.defaultPackageManager.permissionGrants.remove(Kylok.LAN_PERMISSION)
        }
    }

    @Test
    fun testEnrollmentWorkerStoppedAndCancelled() {
        val params = createWorkerParameters()
        val worker = EnrollmentWorker(context, params)

        worker.onStopped()

        // Simulate stopped state on ListenableWorker
        val stopMethod = ListenableWorker::class.java.getDeclaredMethod("stop", Int::class.javaPrimitiveType)
        stopMethod.isAccessible = true
        stopMethod.invoke(worker, 0)

        val result = worker.doWork()
        assertEquals(ListenableWorker.Result.retry(), result)
    }

    private fun createValidEnrollResponse(clientPublicKeyBytes: ByteArray, attempt: String): EnrollResponse {
        val clientPublic = java.security.KeyFactory.getInstance("EC").generatePublic(
            java.security.spec.X509EncodedKeySpec(clientPublicKeyBytes))
        val ephemeralKeyPair = java.security.KeyPairGenerator.getInstance("EC").apply {
            initialize(java.security.spec.ECGenParameterSpec("secp256r1"))
        }.generateKeyPair()

        val agreement = javax.crypto.KeyAgreement.getInstance("ECDH")
        agreement.init(ephemeralKeyPair.private)
        agreement.doPhase(clientPublic, true)
        val shared = agreement.generateSecret()

        val hmac = javax.crypto.Mac.getInstance("HmacSHA256")
        hmac.init(javax.crypto.spec.SecretKeySpec(ByteArray(32), "HmacSHA256"))
        val prk = hmac.doFinal(shared)
        hmac.init(javax.crypto.spec.SecretKeySpec(prk, "HmacSHA256"))
        val key = hmac.doFinal("kylok/enrollment/v1".toByteArray(Charsets.UTF_8) + byteArrayOf(1))

        val devId = "dev-" + "a".repeat(39)
        val scopeId = "scope-test"
        val credentialString = "cred-" + "b".repeat(38)

        val nonce = ByteArray(12).apply { java.security.SecureRandom().nextBytes(this) }
        val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
        cipher.init(javax.crypto.Cipher.ENCRYPT_MODE, javax.crypto.spec.SecretKeySpec(key, "AES"),
            javax.crypto.spec.GCMParameterSpec(128, nonce))
        cipher.updateAAD("kylok/v1\n$devId\n$scopeId\n$attempt".toByteArray(Charsets.UTF_8))
        val ciphertext = cipher.doFinal(credentialString.toByteArray(Charsets.UTF_8))

        return EnrollResponse.newBuilder()
            .setProtocolVersion(BuildConfig.PROTOCOL_VERSION)
            .setDevice(Device.newBuilder().setId(devId).setScopeId(scopeId).setDesiredRevision(1).setState(DeviceState.DEVICE_STATE_MANAGED).build())
            .setCredential(CredentialEnvelope.newBuilder()
                .setEphemeralPublicKey(com.google.protobuf.ByteString.copyFrom(ephemeralKeyPair.public.encoded))
                .setNonce(com.google.protobuf.ByteString.copyFrom(nonce))
                .setCiphertext(com.google.protobuf.ByteString.copyFrom(ciphertext))
                .build())
            .build()
    }

    @Test
    fun testEnrollmentWorkerFullSyncLoop() {
        val devId = "dev-" + "a".repeat(39)
        val scopeId = "scope-test"

        var enrollCalled = false
        var syncCount = 0

        val mockClient = object : AgentClient(null) {
            override fun enroll(token: String, attempt: String, publicKey: ByteArray, inventory: Inventory): EnrollResponse {
                enrollCalled = true
                return createValidEnrollResponse(publicKey, attempt)
            }

            override fun sync(request: SyncRequest, credential: String): SyncResponse {
                syncCount++
                return SyncResponse.newBuilder()
                    .setProtocolVersion(BuildConfig.PROTOCOL_VERSION)
                    .setSequence(request.sequence)
                    .setDevice(Device.newBuilder().setId(devId).setScopeId(scopeId).setDesiredRevision(1).setState(DeviceState.DEVICE_STATE_MANAGED).build())
                    .addAllReceipts(request.reportsList.map { ReportReceipt.newBuilder().setReportId(it.id).build() })
                    .addAllCommandReceipts(request.commandReportsList.map { it.commandId })
                    .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(1).setPolicy(Policy.newBuilder().build()))
                    .build()
            }

            override fun close() {}
        }

        AgentClient.clientFactory = { _, _, _ -> mockClient }

        AgentStorage.update(context) {
            it.put("server", "http://127.0.0.1:8080")
                .put("token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP1")
                .put("attempt", "attempt-1")
            (it as JSONObject).remove("credential")
        }

        val params = createWorkerParameters()
        val worker = EnrollmentWorker(context, params)

        val result = worker.doWork()
        assertEquals(ListenableWorker.Result.success(), result)
        assertTrue(enrollCalled)
        assertEquals(3, syncCount)

        val finalState = AgentStorage.read(context)
        assertEquals("MANAGED", finalState.getString("lifecycle"))
        assertTrue(finalState.getString("result").contains("Authenticated sync revision 1"))

        // Test RPC PERMISSION_DENIED on sync -> REVOKED
        val revokedClient = object : AgentClient(null) {
            override fun sync(request: SyncRequest, credential: String): SyncResponse {
                throw StatusRuntimeException(Status.PERMISSION_DENIED)
            }
            override fun close() {}
        }
        AgentClient.clientFactory = { _, _, _ -> revokedClient }
        val rRevoked = worker.doWork()
        assertEquals(ListenableWorker.Result.failure(), rRevoked)
        assertEquals("REVOKED", AgentStorage.read(context).getString("lifecycle"))

        // Test RPC DEADLINE_EXCEEDED on sync -> ENROLLING, retry
        AgentStorage.update(context) { it.put("lifecycle", "MANAGED") }
        val deadlineClient = object : AgentClient(null) {
            override fun sync(request: SyncRequest, credential: String): SyncResponse {
                throw StatusRuntimeException(Status.DEADLINE_EXCEEDED)
            }
            override fun close() {}
        }
        AgentClient.clientFactory = { _, _, _ -> deadlineClient }
        val rDeadline = worker.doWork()
        assertEquals(ListenableWorker.Result.retry(), rDeadline)

        // Test IOException on sync -> retry
        val ioClient = object : AgentClient(null) {
            override fun sync(request: SyncRequest, credential: String): SyncResponse {
                throw IOException("connection reset")
            }
            override fun close() {}
        }
        AgentClient.clientFactory = { _, _, _ -> ioClient }
        val rIO = worker.doWork()
        assertEquals(ListenableWorker.Result.retry(), rIO)

        // Test unhandled Exception on sync -> failure, ERROR
        val errorClient = object : AgentClient(null) {
            override fun sync(request: SyncRequest, credential: String): SyncResponse {
                throw IllegalStateException("unexpected failure")
            }
            override fun close() {}
        }
        AgentClient.clientFactory = { _, _, _ -> errorClient }
        val rError = worker.doWork()
        assertEquals(ListenableWorker.Result.failure(), rError)
        assertEquals("ERROR", AgentStorage.read(context).getString("lifecycle"))

        // Test sync loop when operations and network_current exist, and pending reports return Result.retry()
        val opTest = Operation.newBuilder().setId("o".repeat(43)).setLeaseId("a".repeat(48))
            .setArtifact(Artifact.newBuilder().setPackageName("com.op.test").setSha256("a".repeat(64)).build())
            .build()
        val dummyNetPolicy = DesiredNetworkPolicy.newBuilder()
            .setRevision(1)
            .setPolicy(NetworkPolicy.getDefaultInstance())
            .build()
        AgentStorage.update(context) {
            it.put("lifecycle", "MANAGED")
                .put("network_current", encode(dummyNetPolicy))
            it.objectValue("operations").put(opTest.id, JSONObject().put("operation", encode(opTest)))
            it.array("reports").put(encode(OperationReport.newBuilder().setId("rep-pending").setOperationId(opTest.id).build()))
        }
        val syncWithPendingClient = object : AgentClient(null) {
            override fun sync(request: SyncRequest, credential: String): SyncResponse {
                return SyncResponse.newBuilder()
                    .setProtocolVersion(BuildConfig.PROTOCOL_VERSION)
                    .setSequence(request.sequence)
                    .setDevice(Device.newBuilder().setId(devId).setScopeId(scopeId).setDesiredRevision(1).setState(DeviceState.DEVICE_STATE_MANAGED).build())
                    .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(1).setPolicy(Policy.getDefaultInstance()).build())
                    .build()
            }
            override fun close() {}
        }
        AgentClient.clientFactory = { _, _, _ -> syncWithPendingClient }
        val rPending = worker.doWork()
        assertEquals(ListenableWorker.Result.retry(), rPending)

        // onStopped
        worker.onStopped()

        // Test capture invalid server URLs (query, fragment, userinfo, path, scheme)
        var waitCount = 0
        val badUrls = listOf(
            "http://example.com?query=1",
            "http://example.com#frag",
            "http://user:pass@example.com",
            "http://example.com/some/path",
            "ftp://example.com"
        )
        for (badUrl in badUrls) {
            val badExtras = PersistableBundle().apply {
                putString("kylok.server", badUrl)
                putString("kylok.enrollment_token", "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOP1")
            }
            val intent = Intent().apply {
                putExtra(DevicePolicyManager.EXTRA_PROVISIONING_ADMIN_EXTRAS_BUNDLE, badExtras)
            }
            var captured: Boolean? = null
            Kylok.capture(context, intent, "test_bad_url") { captured = it }
            Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
            waitCount = 0
            while (captured == null && waitCount < 3000) {
                Thread.sleep(50)
                waitCount += 50
            }
            assertEquals(false, captured)
        }

        // Test capture with empty intent (no extras) when PROVISIONING
        AgentStorage.update(context) { it.put("lifecycle", "PROVISIONING") }
        dpm.owner = true
        var emptyCaptured: Boolean? = null
        Kylok.capture(context, Intent(), "cb_empty") { emptyCaptured = it }
        Kylok.executor.submit {}.get(3, TimeUnit.SECONDS)
        waitCount = 0
        while (emptyCaptured == null && waitCount < 3000) {
            Thread.sleep(50)
            waitCount += 50
        }
        assertEquals(true, emptyCaptured)
        assertEquals("DEVICE_OWNER", AgentStorage.read(context).getString("lifecycle"))
    }
}
