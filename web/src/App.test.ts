import { mount } from '@vue/test-utils'
import App from './App.vue'

describe('Project Hub application shell', () => {
  it('clearly reports that the API is not connected instead of showing sample data', () => {
    const wrapper = mount(App)

    expect(wrapper.get('h1').text()).toBe('Project Hub')
    expect(wrapper.text()).toContain('尚未连接到 API')
    expect(wrapper.findAll('button')).toHaveLength(0)
  })
})
