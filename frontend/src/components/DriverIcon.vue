<!-- A database engine's own logo, or the app's cylinder for one without. -->
<script setup>
import { computed } from 'vue'
import AppLogo from './AppLogo.vue'
import { driverIcon, driverName } from '../utils/drivers'

const props = defineProps({
  driver: { type: String, default: '' },
})

const src = computed(() => driverIcon(props.driver))
</script>

<template>
  <img v-if="src" :src="src" :alt="driverName(driver)" data-test="driver-icon" draggable="false" />
  <AppLogo v-else />
</template>
