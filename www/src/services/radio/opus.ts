/**
 * Opus 音频引擎
 * 处理音频采集、编码、解码和播放
 */

import { OpusEncoder, OpusApplication } from '@minceraftmc/opus-encoder'
import { OpusDecoder } from 'opus-decoder'

// Opus 编码器配置
const OPUS_SAMPLE_RATE = 16000 as const
const OPUS_CHANNELS = 1
const OPUS_FRAME_DURATION = 60 // ms（优化：从 20ms 改为 60ms，减少发包频率）
const OPUS_FRAME_SIZE = OPUS_SAMPLE_RATE * OPUS_FRAME_DURATION / 1000 // 960 samples

// 帧合并配置（优化：2 帧合并发送，降低弱网下的丢包影响）
const OPUS_FRAMES_PER_PACKET = 2 // 每个数据包包含的 Opus 帧数
const OPUS_SEND_INTERVAL = OPUS_FRAME_DURATION * OPUS_FRAMES_PER_PACKET // 120ms

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

  // 【优化】帧合并发送机制
  // 累积 2 个 Opus 帧后合并发送，降低发包频率，改善弱网性能
  private frameAccumulator: Uint8Array[] = [] // 累积的 Opus 帧队列
  private sendIntervalId: ReturnType<typeof setInterval> | null = null
  private readonly SEND_INTERVAL = OPUS_SEND_INTERVAL // 120ms

  // 【关键修复】保存节点引用，以便后续销毁
  // 防止 ScriptProcessorNode 内存泄漏导致重音和卡顿
  private processor: ScriptProcessorNode | null = null
  private source: MediaStreamAudioSourceNode | null = null

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
      // 【修改】将创建的节点赋值给类的私有属性
      // 创建音频源
      this.source = this.audioContext!.createMediaStreamSource(this.mediaStream!)

      // 创建 ScriptProcessor (旧 API，但兼容性好)
      // 实际项目中建议使用 AudioWorklet
      // 【关键修复】：将 4096 改为 1024
      // 1024 样本 @16kHz = 64ms 触发一次，确保数据发送间隔远低于 200ms 超时阈值，消除 UI 闪烁
      this.processor = this.audioContext!.createScriptProcessor(1024, 1, 1)

      this.processor.onaudioprocess = (event) => {
        if (this.state !== 'capturing') return

        const inputData = event.inputBuffer.getChannelData(0)
        this.processAudioData(inputData)
      }

      this.source.connect(this.processor)
      this.processor.connect(this.audioContext!.destination)

      // 【修复爆音方案2】启动发送定时器
      this.startSendTimer()

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

    // 【修复爆音方案2】停止发送定时器
    this.stopSendTimer()

    // 【关键修复：彻底清理节点】
    // 必须在此处断开并销毁音频处理节点，否则下一次 start() 会产生重复的事件监听，
    // 导致同一段音频被捕获多次，进而引发服务器收到的 Opus 数据翻倍产生重音和卡顿。
    if (this.processor) {
      this.processor.disconnect()
      this.processor.onaudioprocess = null
      this.processor = null
    }

    if (this.source) {
      this.source.disconnect()
      this.source = null
    }

    console.log('[AudioCapture] Stopped and nodes cleaned up')
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
   * 【优化】累积 2 帧后合并发送
   */
  private encodeOpus(pcmData: Float32Array): void {
    if (!this.opusEncoder || !this.encoderReady) {
      console.warn('[AudioCapture] Opus encoder not ready, skipping encode')
      return
    }

    try {
      // 使用 Opus 编码器编码 PCM 数据
      const opusFrame = this.opusEncoder.encodeFrame(pcmData)

      if (opusFrame.length > 0) {
        // 累积帧
        this.frameAccumulator.push(opusFrame)

        // 当累积够 2 帧时，合并发送
        if (this.frameAccumulator.length >= OPUS_FRAMES_PER_PACKET) {
          this.sendMergedFrames()
        }
      }
    } catch (error) {
      console.error('[AudioCapture] Opus encode failed:', error)
    }
  }

  /**
   * 【优化的合并发送累积的 Opus 帧
   * 将 2 个 60ms 帧合并为一个 120ms 的数据包发送
   */
  private sendMergedFrames(): void {
    if (this.frameAccumulator.length === 0 || !this.onDataCallback) {
      return
    }

    // 取出累积的帧（最多取 2 帧）
    const framesToSend = this.frameAccumulator.splice(0, OPUS_FRAMES_PER_PACKET)

    if (framesToSend.length === 0) {
      return
    }

    // 计算合并后的总长度
    // 每帧前面加 2 字节的长度前缀
    const headerSize = 2 * framesToSend.length
    const dataLength = framesToSend.reduce((sum, frame) => sum + frame.length, 0)
    const totalLength = headerSize + dataLength

    // 合并帧数据
    const mergedData = new Uint8Array(totalLength)
    let offset = 0
    for (const frame of framesToSend) {
      // 写入帧长度（大端序，2 字节）
      mergedData[offset] = (frame.length >> 8) & 0xFF
      mergedData[offset + 1] = frame.length & 0xFF
      offset += 2
      // 写入帧数据
      mergedData.set(frame, offset)
      offset += frame.length
    }

    // 发送合并后的数据
    this.onDataCallback(mergedData)
  }

  /**
   * 【优化】启动发送定时器
   * 用于在停止录音时刷新剩余未发送的帧
   */
  private startSendTimer(): void {
    if (this.sendIntervalId !== null) {
      return
    }

    // 定期检查是否有需要刷新的剩余帧
    this.sendIntervalId = setInterval(() => {
      // 如果有累积的帧超过一定时间，强制发送
      if (this.frameAccumulator.length > 0 && this.onDataCallback) {
        this.sendMergedFrames()
      }
    }, this.SEND_INTERVAL)
  }

  /**
   * 【优化】停止发送定时器
   */
  private stopSendTimer(): void {
    if (this.sendIntervalId !== null) {
      clearInterval(this.sendIntervalId)
      this.sendIntervalId = null
    }
    // 停止前发送剩余的帧
    if (this.frameAccumulator.length > 0 && this.onDataCallback) {
      this.sendMergedFrames()
    }
    // 清空累积队列
    this.frameAccumulator = []
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
 * 使用预调度机制 (Ahead-of-time Scheduling) 实现无缝拼接播放
 */
export class AudioPlayer {
  private audioContext: AudioContext | null = null
  private state: AudioState = 'idle'
  private onStateChangeCallback: StateChangeCallback | null = null

  // 播放队列
  private audioQueue: AudioBuffer[] = []
  private isPlaying = false
  private nextStartTime = 0

  // --- 抖动缓冲配置 ---
  // 注意：现在每个数据包包含 2 个 60ms Opus 帧（共 120ms），参数已针对合并帧优化
  private maxQueueLength = 8 // 最大队列长度：8 × 120ms ≈ 1 秒，平衡延迟与抗抖动
  private minBufferFrames = 2 // 预缓冲帧数：2 × 120ms = 240ms，对抗网络抖动的初始缓冲
  private isBuffering = true  // 标记当前是否处于"等待缓冲积攒"的状态

  // 音量控制
  private gainNode: GainNode | null = null
  private volume = 0.8

  // Opus 解码器
  private opusDecoder: OpusDecoder<16000> | null = null
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
        const decoder = new OpusDecoder({
          sampleRate: OPUS_SAMPLE_RATE,
          channels: OPUS_CHANNELS,
        })
        await decoder.ready
        this.opusDecoder = decoder
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
   * 【优化】支持解析合并帧格式：[Frame1 Length(2B)][Frame1 Data][Frame2 Length(2B)][Frame2 Data]
   */
  async play(opusData: Uint8Array): Promise<void> {
    await this.init()

    try {
      // 解析合并帧格式
      const frames = this.parseMergedFrames(opusData)

      // 解码所有帧并合并 PCM 数据
      const pcmBuffers: Float32Array[] = []
      let totalSamples = 0

      for (const frame of frames) {
        const pcmData = await this.decodeFrameToPCM(frame)
        if (pcmData) {
          pcmBuffers.push(pcmData)
          totalSamples += pcmData.length
        }
      }

      if (totalSamples === 0) return

      // 合并所有 PCM 数据
      const combinedPCM = new Float32Array(totalSamples)
      let offset = 0
      for (const buffer of pcmBuffers) {
        combinedPCM.set(buffer, offset)
        offset += buffer.length
      }

      // 创建 AudioBuffer
      const audioBuffer = this.createAudioBufferFromPCM(combinedPCM)
      if (audioBuffer) {
        this.queueAudio(audioBuffer)
      }
    } catch (error) {
      console.error('[AudioPlayer] Play failed:', error)
    }
  }

  /**
   * 【优化】解析合并帧格式
   * 格式：[Frame1 Length(2B)][Frame1 Data][Frame2 Length(2B)][Frame2 Data]
   * 兼容单帧格式（无长度前缀）
   */
  private parseMergedFrames(data: Uint8Array): Uint8Array[] {
    const frames: Uint8Array[] = []
    let offset = 0

    // 检查是否是合并帧格式
    // 如果第一个字节的值小于 0x80，很可能是长度前缀（Opus 帧通常以 0x80+ 开头）
    while (offset + 2 <= data.length) {
      const frameLength = (data[offset] << 8) | data[offset + 1]

      // 安全检查：帧长度必须合理
      if (frameLength === 0 || frameLength > 1000 || offset + 2 + frameLength > data.length) {
        // 不是合并帧格式，当作单帧处理
        if (offset === 0) {
          return [data]
        }
        break
      }

      // 提取帧数据
      frames.push(data.slice(offset + 2, offset + 2 + frameLength))
      offset += 2 + frameLength
    }

    // 如果没有解析出任何帧，返回原始数据作为单帧
    if (frames.length === 0) {
      return [data]
    }

    return frames
  }

  /**
   * 编码单个 Opus 帧为 PCM 数据
   */
  private async decodeFrameToPCM(data: Uint8Array): Promise<Float32Array | null> {
    if (!this.audioContext) return null

    try {
      // 优先使用 Opus 解码器
      if (this.decoderReady && this.opusDecoder) {
        const decoded = this.opusDecoder.decodeFrame(data)
        return decoded.channelData[0]
      } else {
        // 回退：假设数据是 Int16 PCM（兼容旧格式）
        const int16Data = new Int16Array(data.buffer, data.byteOffset, data.byteLength / 2)
        const float32Data = new Float32Array(int16Data.length)
        for (let i = 0; i < int16Data.length; i++) {
          float32Data[i] = int16Data[i] / (int16Data[i] < 0 ? 0x8000 : 0x7FFF)
        }
        return float32Data
      }
    } catch (error) {
      console.error('[AudioPlayer] Decode frame failed:', error)
      return null
    }
  }

  /**
   * 从 PCM 数据创建 AudioBuffer
   */
  private createAudioBufferFromPCM(pcmData: Float32Array): AudioBuffer | null {
    if (!this.audioContext) return null

    const audioBuffer = this.audioContext.createBuffer(
      OPUS_CHANNELS,
      pcmData.length,
      OPUS_SAMPLE_RATE
    )
    audioBuffer.getChannelData(0).set(pcmData)
    return audioBuffer
  }

  /**
   * 将 AudioBuffer 加入播放队列
   * 重构缓冲调度机制，实现预缓冲状态机
   */
  private queueAudio(audioBuffer: AudioBuffer): void {
    // 1. 防止内存和延迟无限增长
    if (this.audioQueue.length >= this.maxQueueLength) {
      console.warn('[AudioPlayer] 队列溢出，丢弃最旧的音频帧以追赶实时进度')
      this.audioQueue.shift()
    }

    this.audioQueue.push(audioBuffer)

    // 2. 状态机调度逻辑
    if (this.isBuffering) {
      // 处于缓冲饥饿期，必须等攒够一定帧数才开播
      if (this.audioQueue.length >= this.minBufferFrames) {
        this.isBuffering = false
        this.isPlaying = true
        this.setState('playing')
        this.scheduleQueue() // 调用批量调度
      }
    } else {
      // 非缓冲期，直接将新来的帧安排进底层播放计划
      this.scheduleQueue() // 调用批量调度
    }
  }

  /**
   * 批量调度音频队列
   * 将队列中的音频帧全部提前推入底层音频线程，实现完美的无缝拼接
   */
  private scheduleQueue(): void {
    if (!this.audioContext || !this.gainNode) return

    const currentTime = this.audioContext.currentTime

    // 如果 nextStartTime 落后于当前时间，说明发生了音频饥饿（或者刚起播）
    // 需要重新对齐时间轴，并增加 50ms (0.05秒) 的初始安全缓冲，对抗网络抖动
    if (this.nextStartTime < currentTime) {
      this.nextStartTime = currentTime + 0.05
    }

    // 将队列中所有的帧立刻全部推入 Web Audio API 的调度队列
    while (this.audioQueue.length > 0) {
      const audioBuffer = this.audioQueue.shift()!

      const source = this.audioContext.createBufferSource()
      source.buffer = audioBuffer
      source.connect(this.gainNode)

      // 精确安排在未来的时间点播放，底层会自动严丝合缝地拼接
      source.start(this.nextStartTime)

      // 累加时间，推算下一帧的开始时间
      const nodeEndTime = this.nextStartTime + audioBuffer.duration
      this.nextStartTime = nodeEndTime

      // onended 仅用于检测播放队列是否彻底干涸（饥饿），绝不参与调度
      source.onended = () => {
        // 检查当前时间是否达到了我们计划排期的最后时间
        // 减去 0.01 秒容差，如果达到了，说明底层缓冲已经被彻底播光了
        if (this.audioContext && this.audioContext.currentTime >= this.nextStartTime - 0.01) {
          this.isPlaying = false
          this.isBuffering = true
          this.setState('idle')
        }
      }
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
    this.isBuffering = true
    this.setState('idle')
    console.log('[AudioPlayer] Stopped and queue cleared')
  }

  /**
   * 重置解码器状态
   * 用于 WebSocket 重连或新说话人开始时清除 Opus 解码器的内部状态
   * Opus 解码器是带状态的，如果不重置，旧连接的残留状态会导致重音和卡顿
   */
  resetDecoder(): void {
    if (this.opusDecoder) {
      try {
        // 释放旧的解码器
        this.opusDecoder.free()
        this.opusDecoder = null
        this.decoderReady = false
        console.log('[AudioPlayer] Decoder reset for new stream')
      } catch (error) {
        console.error('[AudioPlayer] Failed to reset decoder:', error)
      }
    }
    // 同时重置播放状态
    this.stop()
  }

  /**
   * 重置播放调度（用于新说话人开始时）
   * 清除队列并重置时间调度，避免旧数据干扰新语音流
   */
  resetSchedule(): void {
    this.audioQueue = []
    this.nextStartTime = 0
    // 重置时必须恢复初始的缓冲保护状态
    this.isBuffering = true
    this.isPlaying = false
    console.log('[AudioPlayer] Schedule reset for new speaker')
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
