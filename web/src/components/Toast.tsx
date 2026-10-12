import { useEffect, useRef } from 'react'

type ToastProps = {
  message: string
  type: 'success' | 'error'
  onClose: () => void
  duration?: number
}

export function Toast({ message, type, onClose, duration = 3000 }: ToastProps) {
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    const timer = setTimeout(() => onCloseRef.current(), duration)
    return () => clearTimeout(timer)
  }, [duration, message, type])

  const bgColor = type === 'success' ? 'bg-success' : 'bg-danger'
  const icon = type === 'success' ? (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
      <path d="M10 0C4.48 0 0 4.48 0 10C0 15.52 4.48 20 10 20C15.52 20 20 15.52 20 10C20 4.48 15.52 0 10 0ZM8 15L3 10L4.41 8.59L8 12.17L15.59 4.58L17 6L8 15Z" fill="currentColor"/>
    </svg>
  ) : (
    <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
      <path d="M10 0C4.48 0 0 4.48 0 10C0 15.52 4.48 20 10 20C15.52 20 20 15.52 20 10C20 4.48 15.52 0 10 0ZM11 15H9V13H11V15ZM11 11H9V5H11V11Z" fill="currentColor"/>
    </svg>
  )

  return (
    <div
      className={`fixed right-6 top-6 z-[9999] flex items-center gap-3 rounded-2xl ${bgColor} px-6 py-4 text-white shadow-lg animate-slide-in`}
      role="alert"
    >
      {icon}
      <span className="font-semibold">{message}</span>
      <button
        type="button"
        onClick={onClose}
        className="ml-2 opacity-70 transition hover:opacity-100"
        aria-label="关闭"
      >
        <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
          <path d="M12 4L4 12M4 4L12 12" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
        </svg>
      </button>
    </div>
  )
}
