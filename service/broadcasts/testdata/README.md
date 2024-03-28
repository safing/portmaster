# Testing Broadcast Notifications

```
# View matching data
curl http://127.0.0.1:817/api/v1/broadcasts/matching-data

# Reset state
curl -X POST http://127.0.0.1:817/api/v1/broadcasts/reset-state

# Simulate notifications
curl --upload-file notifications.yaml http://127.0.0.1:817/api/v1/broadcasts/simulate
```
