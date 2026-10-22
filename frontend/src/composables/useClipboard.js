import { useToasts } from './useToasts'

/** Copy text and say so; a refused clipboard is an error toast, not a throw. */
export async function copyToClipboard(text, label = 'Copied to clipboard') {
  const { notifySuccess, notifyError } = useToasts()
  try {
    await navigator.clipboard.writeText(text)
    notifySuccess(label)
    return true
  } catch {
    notifyError('Could not copy to the clipboard')
    return false
  }
}
