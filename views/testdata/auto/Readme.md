## Readme

Put views on this file and the test will automatically pick it up and run it.

The assertions should be placed in the annotation

```yaml
apiVersion: mission-control.flanksource.com/v1
kind: View
metadata:
  name: test-populate-multi-column
  namespace: test
  uid: 22222222-2222-2222-2222-222222222222
  annotations:
    expected-rows: |
      [
        ["logistics-api-7df4c7f6b7-x9k2m", "Kubernetes::Pod", "Running", "healthy", "missioncontrol", "5", null],
        ["logistics-ui-6c8f9b4d5e-m7n8p", "Kubernetes::Pod", "Running", "healthy", "missioncontrol", "10", null]
      ]
    expected-panels: |
      [
        {
          "name": "Pod Health Summary",
          "type": "number",
          "rows": [
            {"total": 2}
          ]
        }
      ]
```
