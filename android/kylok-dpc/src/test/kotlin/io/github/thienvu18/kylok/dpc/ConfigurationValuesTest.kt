// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import io.github.thienvu18.kylok.proto.v1.*
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test

class ConfigurationValuesTest {
    private class ProcessDeath : Error()
    private fun entry(key: String, value: ConfigurationValue) = ConfigurationEntry.newBuilder().setKey(key).setValue(value).build()
    private fun string(value: String) = ConfigurationValue.newBuilder().setStringValue(value).build()
    @Test fun typedCanonicalRoundTripAndExplicitZeros() {
        val input = ConfigurationBundle.newBuilder()
            .addEntries(entry("z", string("private-value")))
            .addEntries(entry("false", ConfigurationValue.newBuilder().setBooleanValue(false).build()))
            .addEntries(entry("zero", ConfigurationValue.newBuilder().setIntegerValue(0).build()))
            .addEntries(entry("empty", ConfigurationValue.newBuilder().setStringsValue(ConfigurationStrings.getDefaultInstance()).build())).build()
        val normalized = ConfigurationValues.normalize(input)
        assertEquals(listOf("empty", "false", "z", "zero"), normalized.entriesList.map { it.key })
        assertEquals(normalized, ConfigurationValues.read(ConfigurationValues.journal(input)))
        assertEquals(ConfigurationValues.digest(input), ConfigurationValues.digest(normalized))
        assertFalse(ConfigurationValues.digest(input).contains("private-value"))
        try { ConfigurationValues.normalize(input.toBuilder().addEntries(entry("z", string("duplicate"))).build()); fail("duplicate key") } catch (_: IllegalArgumentException) { }
        try { ConfigurationValues.normalize(ConfigurationBundle.newBuilder().addEntries(entry("too_long", string("x".repeat(4097)))).build()); fail("unbounded value") } catch (_: IllegalArgumentException) { }
    }
    @Test fun configurationEffectsJournalRestoreAndSanitizeReports() {
        val app = InstalledApplication.newBuilder().setPackageName("io.test.app").setVersionCode(1).addSigningCertificates("signer").build()
        val values = ConfigurationBundle.newBuilder().addEntries(entry("display_name", string("secret title"))).build()
        val desired = Policy.newBuilder().addRequiredPackages(app.packageName)
            .addManagedConfigurations(ManagedConfiguration.newBuilder().setPackageName(app.packageName).setValues(values)).build()
        val snapshot = SyncResponse.newBuilder().setDevice(Device.newBuilder().setDesiredRevision(2))
            .addDesiredApplications(DesiredApplication.newBuilder().setArtifact(Artifact.newBuilder().setPackageName(app.packageName).setVersionCode(1).addSigningCertificates("signer")))
            .setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(2).setPolicy(desired)).build()
        var state = MemoryState(JSONObject().put("snapshot", encode(snapshot)))
        val baseline = ConfigurationValues.journal(ConfigurationBundle.newBuilder().addEntries(entry("original", string("host baseline"))).build())
        var current = baseline
        var writes = 0
        var crashAfterWrite = true
        var installed = true
        val platform = object : PolicyPlatform {
            override fun now() = 100_000L
            override fun isOwner() = true
            override fun installed(packageName: String) = if (installed) app else null
            override fun check(setting: PolicySetting) { if (!installed) throw PolicyPending("REQUIRED_APP_PENDING") }
            override fun capture(setting: PolicySetting) = current
            override fun observe(setting: PolicySetting) = current
            override fun apply(setting: PolicySetting) {
                assertTrue(state.read().getJSONObject("policy_journal").has(setting.id))
                assertTrue(state.read().getJSONObject("policy_journal").getJSONObject(setting.id).has("attempt_value"))
                current = setting.value; writes++
                if (crashAfterWrite) throw ProcessDeath()
            }
        }
        try { PolicyEngine(state, platform).run(); fail("missing process interruption") } catch (_: ProcessDeath) { }
        state = MemoryState(state.read())
        crashAfterWrite = false
        assertFalse(PolicyEngine(state, platform).run())
        assertEquals(1, writes)
        assertEquals(ConfigurationValues.journal(values), current)
        val report = PolicyReport.parseFrom(decode(state.read().getString("policy_report")))
        val observation = report.resultsList.single { it.ruleId.startsWith("configuration:") }
        assertEquals(ConfigurationValues.digest(values), observation.observedValue)
        assertFalse(report.toString().contains("secret title"))
        assertFalse(PolicyEngine(state, platform).run()); assertEquals(1, writes)
        val replacement = ConfigurationBundle.newBuilder().addEntries(entry("display_name", string("updated title"))).build()
        val replacementPolicy = desired.toBuilder().clearManagedConfigurations()
            .addManagedConfigurations(ManagedConfiguration.newBuilder().setPackageName(app.packageName).setValues(replacement)).build()
        state.update { it.put("snapshot", encode(snapshot.toBuilder().setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(3).setPolicy(replacementPolicy)).setDevice(Device.newBuilder().setDesiredRevision(3)).build())) }
        assertFalse(PolicyEngine(state, platform).run())
        assertEquals(ConfigurationValues.journal(replacement), current)
        assertEquals(2, writes)
        state.update { it.put("snapshot", encode(snapshot.toBuilder().setDesiredPolicy(DesiredPolicy.newBuilder().setRevision(4).setPolicy(Policy.getDefaultInstance())).setDevice(Device.newBuilder().setDesiredRevision(4)).build())) }
        installed = false
        assertTrue(PolicyEngine(state, platform).run())
        assertEquals(1, state.read().getJSONObject("policy_journal").length())
        assertEquals(ConfigurationValues.journal(replacement), current)
        installed = true
        assertFalse(PolicyEngine(state, platform).run())
        assertEquals(baseline, current); assertEquals(3, writes)
        assertEquals(0, state.read().getJSONObject("policy_journal").length())
    }
}
