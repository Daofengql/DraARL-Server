/**
 * Opus 音频引擎
 * 处理音频采集、��码、解码和播放
 */

import { OpusEncoder, OpusApplication } from '@minceraftmc/opus-encoder'
import { OpusDecoder } from 'opus-decoder'

// Opus 编码器配置
const OPUS_SAMPLE_RATE = 16000 as const
const OPUS_CHANNELS = 1
const OPUS_FRAME_DURATION = 20 // ms
const OPUS_FRAME_SIZE = OPUS_SAMPLE_RATE * OPUS_FRAME_DURATION / 1000 // 320 samples

// 音频状态
export type AudioState = 'idle' | 'capturing' | 'playing'

// 回调类型
export type AudioDataCallback = (opusData: Uint8Array) => void
export type StateChangeCallback = (state: AudioState) => void

/**
 * 音频采集器
 * 使用 Web Audio API 采集麦克风音频并编码为 Opus
 */
export class AudioCapture {
  private audioContext: AudioContext | null = null
  private mediaStream: MediaStream | null = null
  private state: AudioState = 'idle'
  private onDataCallback: AudioDataCallback | null = null
  private onStateChangeCallback: StateChangeCallback | null = null

  // 音频处理缓冲
  private buffer: Float32Array[] = []
  private bufferSize = 0
  private targetBufferSize = OPUS_FRAME_SIZE

  // Opus 编码器
  private opusEncoder: OpusEncoder<typeof OPUS_SAMPLE_RATE> | null = null
  private encoderReady = false

  /**
   * 设置数据回调
   */
  onData(callback: AudioDataCallback) {
    this.onDataCallback = callback
  }

  /**
   * 设置状态变化回调
   */
  onStateChange(callback: StateChangeCallback) {
    this.onStateChangeCallback = callback
  }

  /**
   * 获取当前状态
   */
  getState(): AudioState {
    return this.state
  }

  /**
   * 获取可用的输入设备列表
   */
  static async getInputDevices(): Promise<MediaDeviceInfo[]> {
    const devices = await navigator.mediaDevices.enumerateDevices()
    return devices.filter(device => device.kind === 'audioinput')
  }

  /**
   * 初始化音频采集
   */
  async init(deviceId?: string): Promise<void> {
    try {
      // 请求麦克风权限
      const constraints: MediaStreamConstraints = {
        audio: {
          deviceId: deviceId ? { exact: deviceId } : undefined,
          sampleRate: OPUS_SAMPLE_RATE,
          channelCount: OPUS_CHANNELS,
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        }
      }

      this.mediaStream = await navigator.mediaDevices.getUserMedia(constraints)

      // 创建 AudioContext
      this.audioContext = new AudioContext({
        sampleRate: OPUS_SAMPLE_RATE,
        latencyHint: 'interactive',
      })

      // 初始化 Opus 编码器
      await this.initOpusEncoder()

      console.log('[AudioCapture] Initialized with Opus 16K encoder')
    } catch (error) {
      console.error('[AudioCapture] Init failed:', error)
      throw error
    }
  }

  /**
   * 初始化 Opus 编码器
   * 使用 @minceraftmc/opus-encoder 生成原始 Opus 帧
   */
  private async initOpusEncoder(): Promise<void> {
    try {
      this.opusEncoder = new OpusEncoder({
        sampleRate: OPUS_SAMPLE_RATE,
        application: OpusApplication.VOIP,
      })

      // 等待 WASM 编译完成
      await this.opusEncoder.ready
      this.encoderReady = true

      console.log('[AudioCapture] Opus encoder ready (16kHz VOIP mode)')
    } catch (error) {
      console.error('[AudioCapture] Opus encoder init failed:', error)
      throw error
    }
  }

  /**
   * 开始采集
   */
  async start(): Promise<void> {
    if (!this.audioContext || !this.mediaStream) {
      await this.init()
    }

    if (this.state === 'capturing') {
      return
    }

    if (!this.encoderReady || !this.opusEncoder) {
      console.error('[AudioCapture] Opus encoder not ready')
      throw new Error('Opus encoder not ready')
    }

    try {
      // 创建音频源
      const source = this.audioContext!.createMediaStreamSource(this.mediaStream!)

      // 创建 ScriptProcessor (旧 API，但兼容性好)
      // 实际项目中建议使用 AudioWorklet
      const processor = this.audioContext!.createScriptProcessor(4096, 1, 1)

      processor.onaudioprocess = (event) => {
        if (this.state !== 'capturing') return

        const inputData = event.inputBuffer.getChannelData(0)
        this.processAudioData(inputData)
      }

      source.connect(processor)
      processor.connect(this.audioContext!.destination)

      this.setState('capturing')
      console.log('[AudioCapture] Started capturing')

    } catch (error) {
      console.error('[AudioCapture] Start failed:', error)
      throw error
    }
  }

  /**
   * 停止采集
   */
  stop(): void {
    if (this.state !== 'capturing') return

    this.setState('idle')
    this.buffer = []
    this.bufferSize = 0

    console.log('[AudioCapture] Stopped')
  }

  /**
   * 处理音频数据
   */
  private processAudioData(data: Float32Array): void {
    // 将数据添加到缓冲区
    this.buffer.push(new Float32Array(data))
    this.bufferSize += data.length

    // 当缓冲区足够大时，进行编码
    while (this.bufferSize >= this.targetBufferSize) {
      // 合并缓冲区中的数据
      const frame = this.extractFrame()

      // 编码为 Opus
      this.encodeOpus(frame)
    }
  }

  /**
   * 提取一个 Opus 帧的数据
   */
  private extractFrame(): Float32Array {
    const frame = new Float32Array(this.targetBufferSize)
    let offset = 0

    while (offset < this.targetBufferSize && this.buffer.length > 0) {
      const chunk = this.buffer[0]
      const needed = this.targetBufferSize - offset

      if (chunk.length <= needed) {
        frame.set(chunk, offset)
        offset += chunk.length
        this.buffer.shift()
      } else {
        frame.set(chunk.slice(0, needed), offset)
        this.buffer[0] = chunk.slice(needed)
        offset = this.targetBufferSize
      }
    }

    this.bufferSize -= offset
    return frame
  }

  /**
   * 编码为 Opus
   * 使用真正的 Opus 编码器生成原始 Opus 帧
   */
  private encodeOpus(pcmData: Float32Array): void {
    if (!this.opusEncoder || !this.encoderReady) {
      console.warn('[AudioCapture] Opus encoder not ready, skipping encode')
      return
    }

    try {
      // 使用 Opus 编码器编码 PCM 数据
      const opusFrame = this.opusEncoder.encodeFrame(pcmData)

      // 回调发送数据（原始 Opus 帧）
      if (this.onDataCallback && opusFrame.length > 0) {
        this.onDataCallback(opusFrame)
      }
    } catch (error) {
      console.error('[AudioCapture] Opus encode failed:', error)
    }
  }

  /**
   * 设置状态
   */
  private setState(state: AudioState): void {
    if (this.state !== state) {
      this.state = state
      if (this.onStateChangeCallback) {
        this.onStateChangeCallback(state)
      }
    }
  }

  /**
   * 销毁
   */
  destroy(): void {
    this.stop()

    if (this.mediaStream) {
      this.mediaStream.getTracks().forEach(track => track.stop())
      this.mediaStream = null
    }

    if (this.audioContext) {
      this.audioContext.close()
      this.audioContext = null
    }

    if (this.opusEncoder) {
      this.opusEncoder.free()
      this.opusEncoder = null
      this.encoderReady = false
    }
  }
}

/**
 * 音频播放器
 * 解码 Opus 数据并播放
 */
export class AudioPlayer {
  private audioContext: AudioContext | null = null
  private state: AudioState = 'idle'
  private onStateChangeCallback: StateChangeCallback | null = null

  // 播放队列
  private audioQueue: AudioBuffer[] = []
  private isPlaying = false
  private nextStartTime = 0

  // 音量控制
  private gainNode: GainNode | null = null
  private volume = 0.8

  // Opus 解码器
  private opusDecoder: OpusDecoder | null = null
  private decoderReady = false

  /**
   * 设置状态变化回调
   */
  onStateChange(callback: StateChangeCallback) {
    this.onStateChangeCallback = callback
  }

  /**
   * 获取当前状态
   */
  getState(): AudioState {
    return this.state
  }

  /**
   * 初始化
   */
  async init(): Promise<void> {
    if (!this.audioContext) {
      this.audioContext = new AudioContext({
        sampleRate: OPUS_SAMPLE_RATE,
        latencyHint: 'interactive',
      })

      this.gainNode = this.audioContext.createGain()
      this.gainNode.gain.value = this.volume
      this.gainNode.connect(this.audioContext.destination)
    }

    // 恢复 AudioContext（如果被暂停）
    if (this.audioContext.state === 'suspended') {
      await this.audioContext.resume()
    }

    // 初始化 Opus 解码器
    if (!this.decoderReady) {
      try {
        this.opusDecoder = new OpusDecoder({
          sampleRate: OPUS_SAMPLE_RATE,
          channels: OPUS_CHANNELS,
        })
        await this.opusDecoder.ready
        this.decoderReady = true
        console.log('[AudioPlayer] Opus decoder ready (16kHz)')
      } catch (error) {
        console.error('[AudioPlayer] Opus decoder init failed:', error)
        // 解码器初始化失败时，回退到 PCM 模式
      }
    }
  }

  /**
   * 播放 Opus 数据
   */
  async play(opusData: Uint8Array): Promise<void> {
    await this.init()

    try {
      // 解码 Opus 数据为 PCM
      // 注意：这里假设传入的是已经解码的 PCM 数据
      // 实际项目中需要使用 opus-decoder 进行解码

      const audioBuffer = await this.decodeToAudioBuffer(opusData)
      if (audioBuffer) {
        this.queueAudio(audioBuffer)
      }
    } catch (error) {
      console.error('[AudioPlayer] Play failed:', error)
    }
  }

  /**
   * 解码为 AudioBuffer
   */
  private async decodeToAudioBuffer(data: Uint8Array): Promise<AudioBuffer | null> {
    if (!this.audioContext) return null

    try {
      let float32Data: Float32Array

      // 优先使用 Opus 解码器
      if (this.decoderReady && this.opusDecoder) {
        const decoded = this.opusDecoder.decodeFrame(data)
        // decoded.channelData 是一个数组，单声道取第一个元素
        float32Data = decoded.channelData[0]
      } else {
        // 回退：假设数据是 Int16 PCM（兼容旧格式）
        const int16Data = new Int16Array(data.buffer, data.byteOffset, data.byteLength / 2)
        float32Data = new Float32Array(int16Data.length)
        for (let i = 0; i < int16Data.length; i++) {
          float32Data[i] = int16Data[i] / (int16Data[i] < 0 ? 0x8000 : 0x7FFF)
        }
      }

      // 创建 AudioBuffer
      const audioBuffer = this.audioContext.createBuffer(
        OPUS_CHANNELS,
        float32Data.length,
        OPUS_SAMPLE_RATE
      )
      audioBuffer.getChannelData(0).set(float32Data)

      return audioBuffer
    } catch (error) {
      console.error('[AudioPlayer] Decode failed:', error)
      return null
    }
  }

  /**
   * 将 AudioBuffer 加入播放队列
   */
  private queueAudio(audioBuffer: AudioBuffer): void {
    this.audioQueue.push(audioBuffer)

    if (!this.isPlaying) {
      this.playNext()
    }
  }

  /**
   * 播放下一个音频
   */
  private playNext(): void {
    if (!this.audioContext || !this.gainNode || this.audioQueue.length === 0) {
      this.isPlaying = false
      this.setState('idle')
      return
    }

    this.isPlaying = true
    this.setState('playing')

    const audioBuffer = this.audioQueue.shift()!

    // 创建音频源
    const source = this.audioContext.createBufferSource()
    source.buffer = audioBuffer
    source.connect(this.gainNode)

    // 计算播放时间
    const currentTime = this.audioContext.currentTime
    if (this.nextStartTime < currentTime) {
      this.nextStartTime = currentTime
    }

    // 开始播放
    source.start(this.nextStartTime)
    this.nextStartTime += audioBuffer.duration

    // 播放结束后播放下一个
    source.onended = () => {
      this.playNext()
    }
  }

  /**
   * 设置音量
   */
  setVolume(volume: number): void {
    this.volume = Math.max(0, Math.min(1, volume))
    if (this.gainNode) {
      this.gainNode.gain.value = this.volume
    }
  }

  /**
   * 获取音量
   */
  getVolume(): number {
    return this.volume
  }

  /**
   * 静音
   */
  mute(): void {
    if (this.gainNode) {
      this.gainNode.gain.value = 0
    }
  }

  /**
   * 取消静音
   */
  unmute(): void {
    if (this.gainNode) {
      this.gainNode.gain.value = this.volume
    }
  }

  /**
   * 停止播放
   */
  stop(): void {
    this.audioQueue = []
    this.isPlaying = false
    this.nextStartTime = 0
    this.setState('idle')
  }

  /**
   * 设置状态
   */
  private setState(state: AudioState): void {
    if (this.state !== state) {
      this.state = state
      if (this.onStateChangeCallback) {
        this.onStateChangeCallback(state)
      }
    }
  }

  /**
   * 销毁
   */
  destroy(): void {
    this.stop()

    if (this.audioContext) {
      this.audioContext.close()
      this.audioContext = null
    }

    this.gainNode = null

    if (this.opusDecoder) {
      this.opusDecoder.free()
      this.opusDecoder = null
      this.decoderReady = false
    }
  }
}

/**
 * 音频可视化
 * 提供音频波形/频谱数据
 */
export class AudioVisualizer {
  private analyser: AnalyserNode | null = null
  private dataArray: Uint8Array | null = null

  /**
   * 初始化可视化
   */
  init(audioContext: AudioContext, source: AudioNode): void {
    this.analyser = audioContext.createAnalyser()
    this.analyser.fftSize = 256
    this.analyser.smoothingTimeConstant = 0.8

    source.connect(this.analyser)

    this.dataArray = new Uint8Array(this.analyser.frequencyBinCount)
  }

  /**
   * 获取频谱数据
   */
  getFrequencyData(): Uint8Array | null {
    if (!this.analyser || !this.dataArray) return null

    this.analyser.getByteFrequencyData(this.dataArray as Uint8Array<ArrayBuffer>)
    return this.dataArray
  }

  /**
   * 获取波形数据
   */
  getTimeDomainData(): Uint8Array | null {
    if (!this.analyser || !this.dataArray) return null

    this.analyser.getByteTimeDomainData(this.dataArray as Uint8Array<ArrayBuffer>)
    return this.dataArray
  }

  /**
   * 计算音量级别 (0-1)
   */
  getVolumeLevel(): number {
    const data = this.getFrequencyData()
    if (!data) return 0

    let sum = 0
    for (let i = 0; i < data.length; i++) {
      sum += data[i]
    }
    return sum / (data.length * 255)
  }
}

// 导出常量
export { OPUS_SAMPLE_RATE, OPUS_FRAME_SIZE }

// 导出单例工厂
let audioCaptureInstance: AudioCapture | null = null
let audioPlayerInstance: AudioPlayer | null = null

export function getAudioCapture(): AudioCapture {
  if (!audioCaptureInstance) {
    audioCaptureInstance = new AudioCapture()
  }
  return audioCaptureInstance
}

export function getAudioPlayer(): AudioPlayer {
  if (!audioPlayerInstance) {
    audioPlayerInstance = new AudioPlayer()
  }
  return audioPlayerInstance
}

export function destroyAudioInstances(): void {
  if (audioCaptureInstance) {
    audioCaptureInstance.destroy()
    audioCaptureInstance = null
  }
  if (audioPlayerInstance) {
    audioPlayerInstance.destroy()
    audioPlayerInstance = null
  }
}
