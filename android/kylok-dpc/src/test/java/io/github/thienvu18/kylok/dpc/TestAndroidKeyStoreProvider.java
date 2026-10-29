// SPDX-License-Identifier: LGPL-3.0-only
package io.github.thienvu18.kylok.dpc;

import android.security.keystore.KeyGenParameterSpec;
import java.io.InputStream;
import java.io.OutputStream;
import java.security.*;
import java.security.cert.Certificate;
import java.security.spec.AlgorithmParameterSpec;
import java.security.spec.ECGenParameterSpec;
import java.util.*;
import javax.crypto.KeyGeneratorSpi;
import javax.crypto.SecretKey;
import javax.crypto.spec.SecretKeySpec;

/** Custom security provider emulating AndroidKeyStore for unit tests. */
public class TestAndroidKeyStoreProvider extends Provider {
    private static final String PROVIDER_NAME = "AndroidKeyStore";

    public static void install() {
        if (Security.getProvider(PROVIDER_NAME) == null) {
            Security.addProvider(new TestAndroidKeyStoreProvider());
        }
    }

    public static void reset() {
        TestKeyStoreSpi.clear();
    }

    public TestAndroidKeyStoreProvider() {
        super(PROVIDER_NAME, 1.0, "Test AndroidKeyStore Provider");
        put("KeyStore." + PROVIDER_NAME, TestKeyStoreSpi.class.getName());
        put("KeyGenerator.AES", TestAesKeyGeneratorSpi.class.getName());
        put("KeyPairGenerator.EC", TestEcKeyPairGeneratorSpi.class.getName());
    }

    public static class TestKeyStoreSpi extends KeyStoreSpi {
        private static final Map<String, Key> KEYS = new HashMap<>();
        private static final Map<String, Certificate> CERTS = new HashMap<>();

        public static void setKey(String alias, Key key) { KEYS.put(alias, key); }
        public static void setCertificate(String alias, Certificate cert) { CERTS.put(alias, cert); }
        public static void clear() { KEYS.clear(); CERTS.clear(); }

        @Override public Key engineGetKey(String alias, char[] password) { return KEYS.get(alias); }
        @Override public Certificate[] engineGetCertificateChain(String alias) {
            Certificate c = CERTS.get(alias);
            return c != null ? new Certificate[]{c} : null;
        }
        @Override public Certificate engineGetCertificate(String alias) { return CERTS.get(alias); }
        @Override public Date engineGetCreationDate(String alias) { return new Date(); }
        @Override public void engineSetKeyEntry(String alias, Key key, char[] password, Certificate[] chain) {
            KEYS.put(alias, key);
            if (chain != null && chain.length > 0) CERTS.put(alias, chain[0]);
        }
        @Override public void engineSetKeyEntry(String alias, byte[] key, Certificate[] chain) {}
        @Override public void engineSetCertificateEntry(String alias, Certificate cert) { CERTS.put(alias, cert); }
        @Override public void engineDeleteEntry(String alias) { KEYS.remove(alias); CERTS.remove(alias); }
        @Override public Enumeration<String> engineAliases() { return Collections.enumeration(KEYS.keySet()); }
        @Override public boolean engineContainsAlias(String alias) { return KEYS.containsKey(alias); }
        @Override public int engineSize() { return KEYS.size(); }
        @Override public boolean engineIsKeyEntry(String alias) { return KEYS.containsKey(alias); }
        @Override public boolean engineIsCertificateEntry(String alias) { return CERTS.containsKey(alias); }
        @Override public String engineGetCertificateAlias(Certificate cert) { return null; }
        @Override public void engineStore(OutputStream stream, char[] password) {}
        @Override public void engineLoad(InputStream stream, char[] password) {}
    }

    public static class TestAesKeyGeneratorSpi extends KeyGeneratorSpi {
        private String alias;

        @Override protected void engineInit(SecureRandom random) {}
        @Override protected void engineInit(AlgorithmParameterSpec params, SecureRandom random) {
            if (params instanceof KeyGenParameterSpec) {
                alias = ((KeyGenParameterSpec) params).getKeystoreAlias();
            }
        }
        @Override protected void engineInit(int keysize, SecureRandom random) {}
        @Override protected SecretKey engineGenerateKey() {
            byte[] keyBytes = new byte[32];
            new SecureRandom().nextBytes(keyBytes);
            SecretKey key = new SecretKeySpec(keyBytes, "AES");
            if (alias != null) {
                TestKeyStoreSpi.setKey(alias, key);
            }
            return key;
        }
    }

    public static class TestEcKeyPairGeneratorSpi extends KeyPairGeneratorSpi {
        private String alias;

        @Override public void initialize(int keysize, SecureRandom random) {}
        @Override public void initialize(AlgorithmParameterSpec params, SecureRandom random) {
            if (params instanceof KeyGenParameterSpec) {
                alias = ((KeyGenParameterSpec) params).getKeystoreAlias();
            }
        }
        @Override public KeyPair generateKeyPair() {
            try {
                KeyPairGenerator kpg = KeyPairGenerator.getInstance("EC");
                kpg.initialize(new ECGenParameterSpec("secp256r1"));
                KeyPair kp = kpg.generateKeyPair();
                if (alias != null) {
                    TestKeyStoreSpi.setKey(alias, kp.getPrivate());
                    Certificate cert = new Certificate("X.509") {
                        @Override public byte[] getEncoded() { return new byte[0]; }
                        @Override public void verify(PublicKey key) {}
                        @Override public void verify(PublicKey key, String sigProvider) {}
                        @Override public String toString() { return "TestCert"; }
                        @Override public PublicKey getPublicKey() { return kp.getPublic(); }
                    };
                    TestKeyStoreSpi.setCertificate(alias, cert);
                }
                return kp;
            } catch (Exception e) {
                throw new RuntimeException(e);
            }
        }
    }
}
