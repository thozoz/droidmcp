package com.thozoz.droidmcp.helper

import java.io.DataInputStream
import java.io.DataOutputStream
import java.io.EOFException
import java.io.IOException

/** Mirror of daemon/internal/ipc: 4-byte big-endian length, 1-byte type, payload. */
object Frames {
    const val JSON: Byte = 'J'.code.toByte()
    const val BINARY: Byte = 'B'.code.toByte()
    private const val MAX_FRAME = 64 shl 20

    /** Returns null on a clean EOF (the daemon closed the socket). */
    fun read(input: DataInputStream): Pair<Byte, ByteArray>? {
        val len = try {
            input.readInt()
        } catch (e: EOFException) {
            return null
        }
        if (len < 0 || len > MAX_FRAME) throw IOException("frame of $len bytes exceeds limit")
        val type = input.readByte()
        val payload = ByteArray(len)
        input.readFully(payload)
        return type to payload
    }

    fun write(output: DataOutputStream, type: Byte, payload: ByteArray) {
        output.writeInt(payload.size)
        output.writeByte(type.toInt())
        output.write(payload)
    }
}
