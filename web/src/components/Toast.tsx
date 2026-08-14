import { toast } from '../state/ui'

export function Toast() {
  if (!toast.value) return null
  const { message, variant } = toast.value
  return <div class={variant === 'error' ? 'toast toast-error' : 'toast'}>{message}</div>
}
