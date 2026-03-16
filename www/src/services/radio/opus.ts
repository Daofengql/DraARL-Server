/**
 * Opus 音频引擎
 * 处理音频采集、编码、解码和播放
 */

// Opus 编码器配置
const OPUS_SAMPLE_RATE = 16000
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
  private workletNode: AudioWorkletNode | null = null
  private state: AudioState = 'idle'
  private onDataCallback: AudioDataCallback | null = null
  private onStateChangeCallback: StateChangeCallback | null = null

  // 音频处理缓冲
  private buffer: Float32Array[] = []
  private bufferSize = 0
  private targetBufferSize = OPUS_FRAME_SIZE

  // Opus 编码器（使用 opus-recorder 或 opus-encoder）
  private opusEncoder: any = null

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

      console.log('[AudioCapture] Initialized')
    } catch (error) {
      console.error('[AudioCapture] Init failed:', error)
      throw error
    }
  }

  /**
   * 初始化 Opus 编码器
   * 这里使用简化的实现，实际项目中需要引入 opus-encoder 库
   */
  private async initOpusEncoder(): Promise<void> {
    // 检查是否有可用的 Opus 编码器
    // 实际实现中，可以使用 opus-encoder 或 opus-recorder 库
    // 这里我们使用 MediaRecorder 作为后备方案

    try {
      // 尝试使用 MediaRecorder 的 Opus 编码
      const mimeType = MediaRecorder.isTypeSupported('audio/webm;codecs=opus')
        ? 'audio/webm;codecs=opus'
        : 'audio/webm'

      console.log('[AudioCapture] Using MediaRecorder with:', mimeType)

      // 注意：MediaRecorder 生成的是完整的 WebM 容器，不是原始 Opus 帧
      // 对于实时通信，需要使用专门的 Opus 编码器库
      // 这里简化处理，实际项目需要引入 opus-encoder

    } catch (error) {
      console.warn('[AudioCapture] Opus encoder not available:', error)
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
      console.log('[AudioCapture] Started')

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
   * 简化实现，实际需要使用 opus-encoder 库
   */
  private encodeOpus(pcmData: Float32Array): void {
    // 这里应该调用 Opus 编码器
    // 由于浏览器原生不支持 Opus 编码为原始帧，
    // 实际项目中需要引入 opus-encoder 库

    // 简化处理：将 Float32 转换为 Int16 PCM
    const int16Data = new Int16Array(pcmData.length)
    for (let i = 0; i < pcmData.length; i++) {
      const s = Math.max(-1, Math.min(1, pcmData[i]))
      int16Data[i] = s < 0 ? s * 0x8000 : s * 0x7FFF
    }

    // 将 Int16 PCM 转换为 Uint8Array
    const uint8Data = new Uint8Array(int16Data.buffer)

    // 回调发送数据
    // 注意：这不是真正的 Opus 编码，只是 PCM 数据
    // 实际项目中需要使用 opus-encoder 进行编码
    if (this.onDataCallback) {
      this.onDataCallback(uint8Data)
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

    this.opusEncoder = null
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
      // 假设数据是 Int16 PCM
      const int16Data = new Int16Array(data.buffer, data.byteOffset, data.byteLength / 2)

      // 转换为 Float32
      const float32Data = new Float32Array(int16Data.length)
      for (let i = 0; i < int16Data.length; i++) {
        float32Data[i] = int16Data[i] / (int16Data[i] < 0 ? 0x8000 : 0x7FFF)
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
