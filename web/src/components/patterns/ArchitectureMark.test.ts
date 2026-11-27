// @vitest-environment jsdom
import { enableAutoUnmount, mount } from '@vue/test-utils';
import { afterEach, describe, expect, it } from 'vitest';
import ArchitectureMark from './ArchitectureMark.vue';

enableAutoUnmount(afterEach);

describe('ArchitectureMark', () => {
  it('renders the CPU chip glyph plus the readable architecture label', () => {
    const wrapper = mount(ArchitectureMark, {
      props: { architecture: { arch: 'amd64', rawMachine: 'x86_64' } },
    });

    const chip = wrapper.get('[data-testid="arch-mark"]');
    expect(chip.attributes('data-arch-family')).toBe('x86');
    expect(chip.attributes('title')).toBe('amd64 (x86_64)');
    expect(chip.text()).toContain('amd64');
    expect(chip.find('svg')).toBeDefined();
    expect(chip.classes()).toContain('bg-muted');
  });

  it('marks ARM and RISC-V families differently while keeping the label', () => {
    const arm = mount(ArchitectureMark, { props: { architecture: { arch: 'arm64', rawMachine: 'aarch64' } } });
    expect(arm.get('[data-testid="arch-mark"]').attributes('data-arch-family')).toBe('arm64');
    expect(arm.get('[data-testid="arch-mark"]').text()).toContain('arm64');

    const riscv = mount(ArchitectureMark, { props: { architecture: { rawMachine: 'riscv64' } } });
    expect(riscv.get('[data-testid="arch-mark"]').attributes('data-arch-family')).toBe('riscv');
    expect(riscv.get('[data-testid="arch-mark"]').attributes('title')).toBe('riscv64');
  });

  it('renders nothing until the architecture is known', () => {
    const wrapper = mount(ArchitectureMark, { props: { architecture: null } });
    expect(wrapper.find('[data-testid="arch-mark"]').exists()).toBe(false);
  });
});
