import { describe, it, expect } from 'vitest'
import { mount } from '@vue/test-utils'
import HelloWorld from './HelloWorld.vue'

describe('HelloWorld', () => {
  it('renders properly', () => {
    const wrapper = mount(HelloWorld, { props: { msg: 'Hello Vitest' } })
    expect(wrapper.text()).toContain('Hello Vitest')
  })

  it('renders with default styling', () => {
    const wrapper = mount(HelloWorld, { props: { msg: 'Test Message' } })
    expect(wrapper.find('h1').exists()).toBe(true)
    expect(wrapper.find('h1').text()).toBe('Test Message')
  })

  it('contains documentation links', () => {
    const wrapper = mount(HelloWorld, { props: { msg: 'Test' } })
    const links = wrapper.findAll('a')
    expect(links.length).toBeGreaterThan(0)
    
    // Check if Vue documentation link exists
    const vueLink = links.find(link => 
      link.attributes('href')?.includes('vuejs.org')
    )
    expect(vueLink).toBeTruthy()
  })

  it('has proper counter functionality', async () => {
    const wrapper = mount(HelloWorld, { props: { msg: 'Test' } })
    const button = wrapper.find('button')
    
    expect(button.exists()).toBe(true)
    expect(button.text()).toContain('count is')
    
    const initialText = button.text()
    await button.trigger('click')
    
    // Counter should increment
    expect(button.text()).not.toBe(initialText)
  })

  it('responds to prop changes', async () => {
    const wrapper = mount(HelloWorld, { props: { msg: 'Initial' } })
    expect(wrapper.text()).toContain('Initial')
    
    await wrapper.setProps({ msg: 'Updated' })
    expect(wrapper.text()).toContain('Updated')
  })
})
