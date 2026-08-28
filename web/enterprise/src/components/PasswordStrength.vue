<template>
  <div v-if="password" class="password-strength">
    <div class="strength-bar">
      <div
        class="strength-fill"
        :class="strengthClass"
        :style="{ width: strengthPercent + '%' }"
      ></div>
    </div>
    <span class="strength-label" :class="strengthClass">{{ strengthLabel }}</span>
    <ul class="requirements">
      <li :class="req.length ? 'met' : ''">
        <Icon :name="req.length ? 'check' : 'dot'" :size="10" />
        <span>{{ $t('password_strength.min_length') }}</span>
      </li>
      <li :class="req.uppercase ? 'met' : ''">
        <Icon :name="req.uppercase ? 'check' : 'dot'" :size="10" />
        <span>{{ $t('password_strength.uppercase') }}</span>
      </li>
      <li :class="req.lowercase ? 'met' : ''">
        <Icon :name="req.lowercase ? 'check' : 'dot'" :size="10" />
        <span>{{ $t('password_strength.lowercase') }}</span>
      </li>
      <li :class="req.number ? 'met' : ''">
        <Icon :name="req.number ? 'check' : 'dot'" :size="10" />
        <span>{{ $t('password_strength.number') }}</span>
      </li>
    </ul>
  </div>
</template>

<script setup>
import { computed } from 'vue'
import { t } from '@/i18n'
import Icon from '@/components/Icon.vue'

const props = defineProps({
  password: { type: String, default: '' }
})

const req = computed(() => ({
  length: props.password.length >= 8,
  uppercase: /[A-Z]/.test(props.password),
  lowercase: /[a-z]/.test(props.password),
  number: /[0-9]/.test(props.password)
}))

const strengthScore = computed(() => {
  let score = 0
  if (req.value.length) score++
  if (req.value.uppercase) score++
  if (req.value.lowercase) score++
  if (req.value.number) score++
  if (props.password.length >= 12) score++
  return score
})

const strengthClass = computed(() => {
  if (strengthScore.value <= 2) return 'weak'
  if (strengthScore.value <= 4) return 'medium'
  return 'strong'
})

const strengthPercent = computed(() => Math.min(100, strengthScore.value * 20))

const strengthLabel = computed(() => {
  if (strengthClass.value === 'weak') return t('password_strength.weak')
  if (strengthClass.value === 'medium') return t('password_strength.medium')
  return t('password_strength.strong')
})
</script>

<style scoped>
.password-strength {
  margin-top: 6px;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.strength-bar {
  height: 4px;
  background: var(--border);
  border-radius: 2px;
  overflow: hidden;
}

.strength-fill {
  height: 100%;
  border-radius: 2px;
  transition: width .25s ease, background-color .25s ease;
}

.strength-fill.weak { background: var(--fail); }
.strength-fill.medium { background: var(--warn, #f59e0b); }
.strength-fill.strong { background: var(--success, #10b981); }

.strength-label {
  font-size: 11px;
  font-weight: 500;
}

.strength-label.weak { color: var(--fail); }
.strength-label.medium { color: var(--warn, #f59e0b); }
.strength-label.strong { color: var(--success, #10b981); }

.requirements {
  list-style: none;
  margin: 2px 0 0;
  padding: 0;
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 2px 8px;
}

.requirements li {
  display: flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  color: var(--text-3);
}

.requirements li.met {
  color: var(--text-2);
}
</style>
