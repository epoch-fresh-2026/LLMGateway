import { useEffect, useId, useRef, type FormEvent, type ReactNode } from 'react'
import { X } from 'lucide-react'

export function Modal({ title, children, submitText, onClose, onSubmit, wide = false, showActions = true, submitting = false, className = '' }: { title: string; children: ReactNode; submitText?: string; onClose: () => void; onSubmit?: (event: FormEvent<HTMLFormElement>) => void | Promise<void>; wide?: boolean; showActions?: boolean; submitting?: boolean; className?: string }) {
  const titleId = useId()
  const backdrop = useRef<HTMLDivElement>(null)
  const close = useRef(onClose)
  close.current = onClose
  useEffect(() => {
    const previousFocus = document.activeElement as HTMLElement | null
    const container = backdrop.current
    const focusable = () => Array.from(container?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), textarea:not(:disabled), a[href], [tabindex="0"]') ?? [])
    const initialFocus = container?.querySelector<HTMLElement>('[autofocus], input, select, textarea') ?? focusable()[0]
    initialFocus?.focus()
    const handleKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') { event.preventDefault(); close.current() }
      if (event.key !== 'Tab') return
      const items = focusable()
      const first = items[0]
      const last = items[items.length - 1]
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
    }
    document.addEventListener('keydown', handleKey)
    return () => { document.removeEventListener('keydown', handleKey); previousFocus?.focus() }
  }, [])
  const content = <><div className="modal-title"><h3 id={titleId}>{title}</h3><button type="button" className="close" onClick={onClose} aria-label="关闭" title="关闭" disabled={submitting}><X size={18} aria-hidden="true" /></button></div><div className="modal-body">{children}</div>{showActions && <div className="modal-actions"><button type="button" className="button ghost" onClick={onClose} disabled={submitting}>取消</button>{onSubmit && <button className="button primary" type="submit" disabled={submitting}>{submitting ? '创建中…' : submitText || '保存'}</button>}</div>}</>
  const modalClassName = `modal${wide ? ' modal-wide' : ''} ${className}`
  const handleSubmit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault()
    return onSubmit?.(event)
  }
  return <div ref={backdrop} className="modal-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose() }}>{onSubmit ? <form className={modalClassName} role="dialog" aria-modal="true" aria-labelledby={titleId} onSubmit={handleSubmit}>{content}</form> : <div className={modalClassName} role="dialog" aria-modal="true" aria-labelledby={titleId}>{content}</div>}</div>
}
