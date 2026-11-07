import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { ProvisionWifiForm, createWifiFormValue } from './ProvisionWifiForm'
import { ProvisionDraarlForm, createServerFormValue } from './ProvisionDraarlForm'

it('exposes labeled WiFi inputs and prevents duplicate saves while busy', () => {
  const save = vi.fn(); const change = vi.fn()
  const props = { value: createWifiFormValue({ ssid: 'test' }), networks: [], disabled: false, loading: false, helperText: '', onChange: change, onSave: save }
  const { rerender } = render(<ProvisionWifiForm {...props} />)
  fireEvent.click(screen.getByRole('button', { name: /保存 WiFi/ }))
  expect(save).toHaveBeenCalledOnce()
  rerender(<ProvisionWifiForm {...props} loading />)
  expect(screen.getByRole('button', { name: /保存 WiFi/ })).toBeDisabled()
})
it('preserves controlled server edits and labels the save action', () => {
  const change = vi.fn(); const save = vi.fn()
  const value = createServerFormValue({ callsign: 'BH5UVN', nodeSsid: 1 })
  render(<ProvisionDraarlForm value={value} onChange={change} onSave={save} disabled={false} loading={false} />)
  fireEvent.change(screen.getByRole('textbox', { name: 'Callsign' }), { target: { value: 'BH1TEST' } })
  expect(change).toHaveBeenCalledWith({ ...value, callsign: 'BH1TEST' })
  fireEvent.click(screen.getByRole('button', { name: /保存 DraARL/ }))
  expect(save).toHaveBeenCalledOnce()
})
