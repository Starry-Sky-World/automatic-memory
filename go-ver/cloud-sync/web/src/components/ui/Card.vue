<script setup>
import { cn } from '../../lib/utils'

defineProps({
  class: { type: String, default: '' },
})
</script>

<template>
  <section :class="cn('rounded-xl border border-border bg-white p-5 shadow-card', $props.class)">
    <slot />
  </section>
</template>
