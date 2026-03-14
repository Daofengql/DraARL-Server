/**
 * Opus 音频解码工具
 * 用于解码服务端存储的 Raw Opus 格式音频文件
 *
 * Raw Opus 文件格式:
 * - 头部 24 字节:
 *   - Magic: "OPUS" (4B)
 *   - Version: 1 (2B, uint16 LE)
 *   - SampleRate: 16000 (4B, uint32 LE)
 *   - Channels: 1 (2B, uint16 LE)
 *   - FrameSize: 320 (2B, uint16 LE)
 *   - FrameCount: N (4B, uint32 LE)
 *   - Reserved: 0 (6B)
 * - 帧数据:
 *   - [长度(2B)][Opus 数据][长度(2B)][Opus]...
 */

import { OpusDecoder } from 'opus-decoder'

// Raw Opus 文件头结构
interface RawOpusHeader {
  magic: string
  version: number
  sampleRate: number
  channels: number
  frameSize: number
  frameCount: number
}

// 解码器接口
interface Decoder {
  decodeFrame: (frame: Uint8Array) => Promise<{ channelData: Float32Array[]; samplesDecoded: number; sampleRate: number }>
  decodeFrames: (frames: Uint8Array[]) => Promise<{ channelData: Float32Array[]; samplesDecoded: number; sampleRate: number }>
  ready: Promise<void>
  reset: () => Promise<void>
  free: () => void
}

// 解码器实例缓存
let decoderInstance: Decoder | null = null
let decoderReady = false

/**
 * 初始化 Opus 解码器
 */
async function initDecoder(): Promise<Decoder> {
  if (decoderInstance && decoderReady) {
    return decoderInstance
  }

  // opus-decoder 库使用默认 48kHz 内部采样率
  decoderInstance = new OpusDecoder({
    channels: 1,
  }) as unknown as Decoder

  await decoderInstance.ready
  decoderReady = true

  return decoderInstance
}

/**
 * 解析 Raw Opus 文件头
 */
function parseHeader(data: ArrayBuffer): { header: RawOpusHeader; dataOffset: number } {
  const view = new DataView(data)

  // 读取 Magic
  const magicBytes = new Uint8Array(data, 0, 4)
  const magic = String.fromCharCode(...magicBytes)

  if (magic !== 'OPUS') {
    throw new Error(`Invalid Opus file format: expected "OPUS", got "${magic}"`)
  }

  const header: RawOpusHeader = {
    magic,
    version: view.getUint16(4, true), // little-endian
    sampleRate: view.getUint32(6, true),
    channels: view.getUint16(10, true),
    frameSize: view.getUint16(12, true),
    frameCount: view.getUint32(14, true),
  }

  return { header, dataOffset: 24 }
}

/**
 * 从 Raw Opus 数据中提取所有帧
 */
function extractFrames(data: ArrayBuffer, header: RawOpusHeader, dataOffset: number): Uint8Array[] {
  const frames: Uint8Array[] = []
  const view = new DataView(data)
  let offset = dataOffset

  for (let i = 0; i < header.frameCount; i++) {
    if (offset + 2 > data.byteLength) {
      console.warn(`Frame ${i}: insufficient data for frame length`)
      break
    }

    // 读取帧长度 (2 bytes, little-endian)
    const frameLen = view.getUint16(offset, true)
    offset += 2

    if (offset + frameLen > data.byteLength) {
      console.warn(`Frame ${i}: insufficient data for frame content`)
      break
    }

    // 提取帧数据
    const frame = new Uint8Array(data, offset, frameLen)
    frames.push(frame)
    offset += frameLen
  }

  return frames
}

/**
 * 解码 Raw Opus 文件为 PCM 数据
 * @param arrayBuffer Raw Opus 文件的 ArrayBuffer
 * @returns PCM 音频数据 (Float32Array 格式，用于 Web Audio API)
 */
export async function decodeRawOpus(arrayBuffer: ArrayBuffer): Promise<{
  pcmData: Float32Array[]
  sampleRate: number
  channels: number
  duration: number
}> {
  // 解析文件头
  const { header, dataOffset } = parseHeader(arrayBuffer)

  if (header.version !== 1) {
    throw new Error(`Unsupported Opus file version: ${header.version}`)
  }

  // 提取所有帧
  const frames = extractFrames(arrayBuffer, header, dataOffset)

  if (frames.length === 0) {
    throw new Error('No Opus frames found in file')
  }

  // 初始化解码器
  const decoder = await initDecoder()

  // 使用 decodeFrames 批量解码所有帧（更高效）
  const { channelData, samplesDecoded, sampleRate } = await decoder.decodeFrames(frames)

  if (samplesDecoded === 0 || !channelData || channelData.length === 0) {
    throw new Error('Failed to decode any frames')
  }

  // 对于单声道，取第一个通道的数据
  const pcmChunks: Float32Array[] = [channelData[0]]

  // 计算总时长 (秒) - 注意：解码后的采样率是 48000
  const totalSamples = samplesDecoded
  const duration = totalSamples / sampleRate

  return {
    pcmData: pcmChunks,
    sampleRate: sampleRate, // 使用解码器返回的实际采样率 (48000)
    channels: header.channels,
    duration,
  }
}

/**
 * 将 PCM 数据转换为 WAV 格式 Blob
 */
export function pcmToWav(
  pcmChunks: Float32Array[],
  sampleRate: number,
  channels: number
): Blob {
  // 合并所有 PCM 数据
  const totalLength = pcmChunks.reduce((sum, chunk) => sum + chunk.length, 0)
  const mergedPcm = new Float32Array(totalLength)
  let offset = 0
  for (const chunk of pcmChunks) {
    mergedPcm.set(chunk, offset)
    offset += chunk.length
  }

  // 转换为 16-bit PCM
  const int16Pcm = new Int16Array(mergedPcm.length)
  for (let i = 0; i < mergedPcm.length; i++) {
    const s = Math.max(-1, Math.min(1, mergedPcm[i]))
    int16Pcm[i] = s < 0 ? s * 0x8000 : s * 0x7fff
  }

  // 创建 WAV 文件
  const wavBuffer = new ArrayBuffer(44 + int16Pcm.length * 2)
  const view = new DataView(wavBuffer)

  // RIFF chunk
  writeString(view, 0, 'RIFF')
  view.setUint32(4, 36 + int16Pcm.length * 2, true)
  writeString(view, 8, 'WAVE')

  // fmt chunk
  writeString(view, 12, 'fmt ')
  view.setUint32(16, 16, true) // chunk size
  view.setUint16(20, 1, true) // PCM format
  view.setUint16(22, channels, true)
  view.setUint32(24, sampleRate, true)
  view.setUint32(28, sampleRate * channels * 2, true) // byte rate
  view.setUint16(32, channels * 2, true) // block align
  view.setUint16(34, 16, true) // bits per sample

  // data chunk
  writeString(view, 36, 'data')
  view.setUint32(40, int16Pcm.length * 2, true)

  // 写入 PCM 数据
  const pcmBytes = new Uint8Array(wavBuffer, 44)
  const pcmView = new DataView(int16Pcm.buffer)
  for (let i = 0; i < int16Pcm.length; i++) {
    pcmBytes[i * 2] = pcmView.getUint8(i * 2)
    pcmBytes[i * 2 + 1] = pcmView.getUint8(i * 2 + 1)
  }

  return new Blob([wavBuffer], { type: 'audio/wav' })
}

function writeString(view: DataView, offset: number, str: string) {
  for (let i = 0; i < str.length; i++) {
    view.setUint8(offset + i, str.charCodeAt(i))
  }
}

/**
 * 使用 Web Audio API 播放 Raw Opus 音频
 */
export class OpusPlayer {
  private audioContext: AudioContext | null = null
  private sourceNode: AudioBufferSourceNode | null = null
  private isPlaying = false

  /**
   * 播放 Raw Opus 音频
   * @param url Raw Opus 文件的 URL
   * @param onEnded 播放结束回调
   */
  async play(url: string, onEnded?: () => void): Promise<void> {
    // 停止当前播放
    this.stop()

    try {
      // 获取音频数据
      const response = await fetch(url)
      if (!response.ok) {
        throw new Error(`Failed to fetch audio: ${response.status}`)
      }

      const arrayBuffer = await response.arrayBuffer()

      // 解码 Opus
      const { pcmData, sampleRate, channels } = await decodeRawOpus(arrayBuffer)

      // 创建 AudioContext
      this.audioContext = new AudioContext({ sampleRate })

      // 合并 PCM 数据
      const totalLength = pcmData.reduce((sum, chunk) => sum + chunk.length, 0)
      const mergedPcm = new Float32Array(totalLength)
      let offset = 0
      for (const chunk of pcmData) {
        mergedPcm.set(chunk, offset)
        offset += chunk.length
      }

      // 创建 AudioBuffer
      const audioBuffer = this.audioContext.createBuffer(
        channels,
        mergedPcm.length,
        sampleRate
      )
      audioBuffer.copyToChannel(mergedPcm, 0)

      // 创建并连接源节点
      this.sourceNode = this.audioContext.createBufferSource()
      this.sourceNode.buffer = audioBuffer
      this.sourceNode.connect(this.audioContext.destination)

      // 设置结束回调
      this.sourceNode.onended = () => {
        this.isPlaying = false
        onEnded?.()
      }

      // 开始播放
      this.sourceNode.start()
      this.isPlaying = true
    } catch (err) {
      console.error('Failed to play Opus audio:', err)
      this.stop()
      throw err
    }
  }

  /**
   * 停止播放
   */
  stop(): void {
    if (this.sourceNode) {
      try {
        this.sourceNode.stop()
      } catch (e) {
        // 忽略已停止的错误
      }
      this.sourceNode.disconnect()
      this.sourceNode = null
    }

    if (this.audioContext) {
      this.audioContext.close()
      this.audioContext = null
    }

    this.isPlaying = false
  }

  /**
   * 获取播放状态
   */
  getIsPlaying(): boolean {
    return this.isPlaying
  }
}

/**
 * 获取 Raw Opus 音频的 WAV 格式 URL（用于下载或兼容播放）
 */
export async function getWavBlobFromOpusUrl(url: string): Promise<Blob> {
  const response = await fetch(url)
  if (!response.ok) {
    throw new Error(`Failed to fetch audio: ${response.status}`)
  }

  const arrayBuffer = await response.arrayBuffer()
  const { pcmData, sampleRate, channels } = await decodeRawOpus(arrayBuffer)

  return pcmToWav(pcmData, sampleRate, channels)
}

// 导出单例播放器
export const opusPlayer = new OpusPlayer()
