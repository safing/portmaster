<template>
  <div class="warning-card">
    <div class="header">
      <span class="icon">⚠️</span>
      <h2>Suspicious Activity Detected</h2>
    </div>
    <div class="content">
      <p><strong>{{ processName }}</strong> is exhibiting suspicious behavior.</p>
      <p>PID: {{ pid }} | Anomaly Score: {{ score }}</p>
    </div>
    <div class="actions">
      <button @click="quarantineApp" :disabled="quarantined">
        {{ quarantined ? 'App Quarantined' : 'Quarantine App' }}
      </button>
    </div>
    <div v-if="error" class="error">
      {{ error }}
    </div>
  </div>
</template>

<script>
export default {
  name: 'WarningCard',
  props: {
    processName: String,
    pid: Number,
    score: Number
  },
  data() {
    return {
      quarantined: false,
      error: null
    }
  },
  methods: {
    async quarantineApp() {
      try {
        const formData = new FormData();
        // Since the UI doesn't inherently have the profile ID linked, we simulate passing it.
        // In full impl, this would resolve the PID -> Profile ID. For now we pass the binary path as fallback.
        formData.append('profile', this.processName);

        const res = await fetch('http://127.0.0.1:817/api/v1/hids/quarantine', {
          method: 'POST',
          body: formData
        });

        if (res.ok) {
          this.quarantined = true;
          this.error = null;
        } else {
          throw new Error("Failed to quarantine");
        }
      } catch (err) {
        this.error = "Error attempting to quarantine application: " + err.message;
      }
    }
  }
}
</script>

<style scoped>
.warning-card {
  border: 2px solid #e74c3c;
  border-radius: 8px;
  padding: 16px;
  margin: 16px 0;
  background-color: #fdf2f0;
}
.header {
  display: flex;
  align-items: center;
  color: #c0392b;
}
.header h2 {
  margin: 0 0 0 10px;
}
.icon {
  font-size: 24px;
}
.content p {
  margin: 8px 0;
}
.actions button {
  background-color: #e74c3c;
  color: white;
  border: none;
  padding: 10px 16px;
  border-radius: 4px;
  cursor: pointer;
  font-weight: bold;
}
.actions button:disabled {
  background-color: #95a5a6;
  cursor: not-allowed;
}
.error {
  color: red;
  margin-top: 10px;
}
</style>
