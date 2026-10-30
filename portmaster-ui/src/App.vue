<template>
  <div class="app-container">
    <h1>Portmaster HIDS Interface</h1>
    <WarningCard
      v-for="alert in alerts"
      :key="alert.pid"
      :processName="alert.binaryPath"
      :pid="alert.pid"
      :score="alert.score"
    />
  </div>
</template>

<script>
import WarningCard from './components/WarningCard.vue';

export default {
  name: 'App',
  components: {
    WarningCard
  },
  data() {
    return {
      alerts: []
    }
  },
  mounted() {
    // Dummy initial state to show UI, typically this would connect via WebSocket
    this.alerts = [
      { pid: 1042, binaryPath: "/usr/bin/curl", score: 0.85 }
    ];
  }
}
</script>

<style>
.app-container {
  font-family: Arial, sans-serif;
  padding: 20px;
}
</style>
