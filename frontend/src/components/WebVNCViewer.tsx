import { useEffect, useRef, useState } from 'react'
import { Monitor, RefreshCw, Send, X } from 'lucide-react'
import RFB from '@novnc/novnc'
import { createVNCTicket, getWebVNCUrl } from '../services/api'

interface WebVNCViewerProps {
  containerName: string
  onClose: () => void
}

export default function WebVNCViewer({ containerName, onClose }: WebVNCViewerProps) {
  const screenRef = useRef<HTMLDivElement>(null)
  const rfbRef = useRef<RFB | null>(null)
  const [status, setStatus] = useState<'connecting' | 'connected' | 'disconnected' | 'error'>('connecting')
  const [errorMsg, setErrorMsg] = useState('')

  const cleanup = () => {
    if (rfbRef.current) {
      rfbRef.current.disconnect()
      rfbRef.current = null
    }
  }

  const ensureResizeObserver = () => {
    if ('ResizeObserver' in window) return

    class FallbackResizeObserver {
      private target: Element | null = null
      private timer = 0
      private lastWidth = -1
      private lastHeight = -1

      constructor(private callback: ResizeObserverCallback) {}

      observe = (target: Element) => {
        this.target = target
        this.check()
        this.timer = window.setInterval(this.check, 250)
        window.addEventListener('resize', this.check)
      }

      unobserve = () => this.disconnect()

      disconnect = () => {
        if (this.timer) window.clearInterval(this.timer)
        this.timer = 0
        window.removeEventListener('resize', this.check)
        this.target = null
      }

      private check = () => {
        if (!this.target) return
        const contentRect = this.target.getBoundingClientRect()
        if (contentRect.width === this.lastWidth && contentRect.height === this.lastHeight) return
        this.lastWidth = contentRect.width
        this.lastHeight = contentRect.height
        this.callback([{ target: this.target, contentRect } as ResizeObserverEntry], this as unknown as ResizeObserver)
      }
    }

    ;(window as unknown as { ResizeObserver: typeof ResizeObserver }).ResizeObserver = FallbackResizeObserver as unknown as typeof ResizeObserver
  }

  const connect = async () => {
    const target = screenRef.current
    if (!target) return

    cleanup()
    target.innerHTML = ''
    setStatus('connecting')
    setErrorMsg('')

    let ticket = ''
    try {
      const response = await createVNCTicket(containerName)
      ticket = response.data.data?.ticket || ''
    } catch (err: unknown) {
      const error = err as { response?: { data?: { message?: string } } }
      setStatus('error')
      setErrorMsg(error.response?.data?.message || 'WebVNC ticket 创建失败，请重新登录后再试')
      return
    }
    if (!ticket) {
      setStatus('error')
      setErrorMsg('WebVNC ticket 为空，请重新登录后再试')
      return
    }

    try {
      ensureResizeObserver()
      const rfb = new RFB(target, getWebVNCUrl(containerName), {
        wsProtocols: ['binary', `clicd-vnc-ticket.${ticket}`],
      })
      rfb.scaleViewport = true
      rfb.resizeSession = false
      rfb.focusOnClick = true
      rfb.qualityLevel = 6
      rfb.compressionLevel = 2
      rfb.background = '#050505'
      rfb.addEventListener('connect', () => {
        setStatus('connected')
      })
      rfb.addEventListener('disconnect', (event) => {
        const detail = (event as CustomEvent<{ clean?: boolean }>).detail
        setStatus((current) => current === 'error' ? current : 'disconnected')
        if (detail && detail.clean === false) {
          setErrorMsg('WebVNC 连接已断开，请确认虚拟机正在运行且 VNC 控制台可用')
        }
      })
      rfb.addEventListener('securityfailure', () => {
        setStatus('error')
        setErrorMsg('VNC 安全协商失败')
      })
      rfb.addEventListener('credentialsrequired', () => {
        setStatus('error')
        setErrorMsg('当前 VNC 控制台要求密码，暂不支持自动输入')
      })
      rfbRef.current = rfb
    } catch (err) {
      console.error(err)
      setStatus('error')
      const message = err instanceof Error && err.message ? `：${err.message}` : ''
      setErrorMsg(`WebVNC 初始化失败${message}`)
    }
  }

  useEffect(() => {
    const timer = window.setTimeout(connect, 100)
    return () => {
      window.clearTimeout(timer)
      cleanup()
    }
  }, [containerName])

  return (
    <div className="bg-white border border-gray-200 rounded-lg overflow-hidden h-full flex flex-col">
      <div className="flex items-center justify-between px-4 py-2.5 border-b border-gray-200 bg-gray-50 shrink-0">
        <div className="flex items-center gap-2">
          <Monitor className="w-4 h-4 text-gray-600" />
          <span className="text-sm font-medium text-black">WebVNC - {containerName}</span>
          {status === 'connected' && <span className="text-xs px-1.5 py-0.5 rounded bg-green-100 text-green-700">已连接</span>}
          {status === 'connecting' && <span className="text-xs px-1.5 py-0.5 rounded bg-yellow-100 text-yellow-700">连接中...</span>}
          {status === 'disconnected' && <span className="text-xs px-1.5 py-0.5 rounded bg-gray-100 text-gray-600">已断开</span>}
          {status === 'error' && <span className="text-xs px-1.5 py-0.5 rounded bg-red-100 text-red-700">连接失败</span>}
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={() => rfbRef.current?.sendCtrlAltDel()}
            className="inline-flex items-center gap-1 px-2 py-1.5 hover:bg-gray-200 rounded text-gray-500 text-xs"
            title="发送 Ctrl+Alt+Del"
          >
            <Send className="w-3.5 h-3.5" />
            Ctrl+Alt+Del
          </button>
          <button onClick={connect} className="p-1.5 hover:bg-gray-200 rounded text-gray-500 text-xs" title="重新连接">
            <RefreshCw className="w-3.5 h-3.5" />
          </button>
          <button onClick={onClose} className="p-1.5 hover:bg-gray-200 rounded text-gray-500" title="关闭">
            <X className="w-4 h-4" />
          </button>
        </div>
      </div>

      <div className="relative flex-1 min-h-0 bg-black overflow-hidden">
        <div ref={screenRef} className="h-full w-full [&>div]:h-full [&>div]:w-full [&_canvas]:block" />
        {(status === 'connecting' || status === 'error' || (status === 'disconnected' && errorMsg)) && (
          <div className={`absolute inset-x-0 bottom-0 border-t px-4 py-2 text-sm ${status === 'error' ? 'border-red-900 bg-red-950 text-red-100' : 'border-gray-800 bg-gray-950 text-gray-200'}`}>
            {status === 'connecting' ? '正在连接 KVM VNC 控制台...' : (errorMsg || 'WebVNC 已断开')}
          </div>
        )}
      </div>
    </div>
  )
}
