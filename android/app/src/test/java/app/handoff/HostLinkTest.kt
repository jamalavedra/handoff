package app.handoff

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class HostLinkTest {
    @Test
    fun normalizeHost() {
        assertEquals("https://acme.handoff.app", normalizeHost("acme.handoff.app"))
        assertEquals("https://acme.handoff.app", normalizeHost(" https://acme.handoff.app/ "))
        assertEquals("http://acme.localhost:8080", normalizeHost("http://acme.localhost:8080"))
        assertEquals("https://acme.handoff.app", normalizeHost("acme.handoff.app/"))
    }

    @Test
    fun linkToken() {
        assertEquals("abc123", linkToken("http://acme.handoff.localhost:8080/auth/verify?token=abc123"))
        assertEquals("abc123", linkToken("https://h/auth/verify?x=1&token=abc123"))
        assertEquals("aB_c-d", linkToken("aB_c-d")) // bare token
        assertNull(linkToken(""))
        assertNull(linkToken("https://h/auth/verify?nope=1"))
        assertNull(linkToken("not a token!"))
    }
}
