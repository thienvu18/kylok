// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc

import android.os.Bundle
import android.os.Parcelable
import io.github.thienvu18.kylok.proto.v1.*

/** Preserve all Android application-restriction value types through typed protobuf, without JSON type inference. */
internal object AndroidConfigurations {
    @Suppress("DEPRECATION")
    /** Encode existing settings for this administrator's durable baseline; reject unsupported values before effects. */
    fun capture(bundle: Bundle): ConfigurationBundle = captureDepth(bundle, 1)
    @Suppress("DEPRECATION")
    /** Capture one bounded nesting level from the existing administrator bundle. */
    private fun captureDepth(bundle: Bundle, depth: Int): ConfigurationBundle {
        if (depth > 4) throw PolicyUnsupported("CONFIGURATION_BASELINE_DEPTH_UNSUPPORTED")
        val result = ConfigurationBundle.newBuilder()
        for (key in bundle.keySet().sorted()) {
            val value = ConfigurationValue.newBuilder()
            when (val item = bundle.get(key)) {
                is Boolean -> value.booleanValue = item
                is Int -> value.integerValue = item
                is String -> value.stringValue = item
                is Bundle -> value.bundleValue = captureDepth(item, depth + 1)
                is Array<*> -> when {
                    item.isArrayOf<String>() -> value.stringsValue = ConfigurationStrings.newBuilder().addAllValues(item.map { it as String }).build()
                    item.isArrayOf<Parcelable>() && item.all { it is Bundle } -> value.bundlesValue = ConfigurationBundles.newBuilder().addAllValues(item.map { captureDepth(it as Bundle, depth + 1) }).build()
                    else -> throw PolicyUnsupported("CONFIGURATION_BASELINE_TYPE_UNSUPPORTED")
                }
                else -> throw PolicyUnsupported("CONFIGURATION_BASELINE_TYPE_UNSUPPORTED")
            }
            result.addEntries(ConfigurationEntry.newBuilder().setKey(key).setValue(value))
        }
        return ConfigurationValues.normalize(result.build())
    }
    /** Decode a validated typed bundle immediately before DevicePolicyManager applies it. */
    fun bundle(input: ConfigurationBundle): Bundle = Bundle().apply {
        for (entry in ConfigurationValues.normalize(input).entriesList) {
            val value = entry.value
            when (value.valueCase) {
                ConfigurationValue.ValueCase.BOOLEAN_VALUE -> putBoolean(entry.key, value.booleanValue)
                ConfigurationValue.ValueCase.INTEGER_VALUE -> putInt(entry.key, value.integerValue)
                ConfigurationValue.ValueCase.STRING_VALUE -> putString(entry.key, value.stringValue)
                ConfigurationValue.ValueCase.STRINGS_VALUE -> putStringArray(entry.key, value.stringsValue.valuesList.toTypedArray())
                ConfigurationValue.ValueCase.BUNDLE_VALUE -> putBundle(entry.key, bundle(value.bundleValue))
                ConfigurationValue.ValueCase.BUNDLES_VALUE -> putParcelableArray(entry.key, value.bundlesValue.valuesList.map { bundle(it) }.toTypedArray())
                else -> error("Unsupported validated configuration")
            }
        }
    }
}
