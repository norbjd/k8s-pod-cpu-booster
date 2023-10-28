package informer

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Test_getBoostMultiplierFromAnnotations(t *testing.T) {
	t.Run("should take the default value if no annotation is provided", func(t *testing.T) {
		boostMultiplier := getBoostMultiplierFromAnnotations(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: nil,
			},
		})
		assert.Equal(t, cpuBoostDefaultMultiplier, boostMultiplier)
	})

	t.Run("should take the value if annotation is valid", func(t *testing.T) {
		notDefaultValue := uint64(5)
		notDefaultValueString := fmt.Sprintf("%d", notDefaultValue)
		require.NotEqual(t, cpuBoostDefaultMultiplier, notDefaultValueString, "must not use the default value in that test!")

		boostMultiplier := getBoostMultiplierFromAnnotations(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"norbjd.github.io/k8s-pod-cpu-booster-multiplier": notDefaultValueString,
				},
			},
		})
		assert.Equal(t, notDefaultValue, boostMultiplier)
	})

	t.Run("should fail if annotation value is invalid", func(t *testing.T) {
		boostMultiplier := getBoostMultiplierFromAnnotations(&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					"norbjd.github.io/k8s-pod-cpu-booster-multiplier": "not-a-valid-value",
				},
			},
		})
		assert.Equal(t, cpuBoostDefaultMultiplier, boostMultiplier)
	})
}
