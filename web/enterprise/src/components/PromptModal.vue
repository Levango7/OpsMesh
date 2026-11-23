<template>
  <Teleport to="body">
    <transition name="modal">
      <div v-if="modelValue" class="modal-overlay" :data-testid="testId" @click.self="onCancel">
        <div
          ref="dialogRef"
          class="modal-box"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleId"
          :aria-describedby="message ? messageId : undefined"
          tabindex="-1"
        >
          <h3 :id="titleId" class="modal-title">{{ title }}</h3>
          <p v-if="message" :id="messageId" class="modal-message modal-message--tight">{{ message }}</p>
          <input
            ref="inputRef"
            v-model="inputValue"
            class="modal-input"
            data-testid="prompt-modal-input"
            :placeholder="placeholder"
            :aria-label="message || title"
            @keyup.enter="onConfirm"
          />
          <div class="modal-actions">
            <button class="outline" @click="onCancel" data-testid="prompt-modal-cancel">
              {{ cancelText || $t('common.cancel') }}
            </button>
            <button class="primary" @click="onConfirm" data-testid="prompt-modal-confirm">
              {{ confirmText || $t('common.confirm') }}
            </button>
          </div>
        </div>
      </div>
    </transition>
  </Teleport>
</template>

<script setup>
// 通用输入对话框 — 替换原生 prompt()
// 通过 v-model 控制显示，emit confirm(value) / cancel
// 打开时自动聚焦输入框并选中默认值，回车确认
import { ref, watch, nextTick, onBeforeUnmount } from 'vue'
import { trapFocus } from '@/utils/a11y'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: '' },
  message: { type: String, default: '' },
  placeholder: { type: String, default: '' },
  defaultValue: { type: String, default: '' },
  confirmText: { type: String, default: '' },
  cancelText: { type: String, default: '' },
  // 同一页面存在多个 PromptModal 时传入不同 testId，便于 e2e 精确定位
  testId: { type: String, default: 'prompt-modal' }
})

const emit = defineEmits(['update:modelValue', 'confirm', 'cancel'])

const inputRef = ref(null)
const dialogRef = ref(null)
const inputValue = ref('')
const uid = Math.random().toString(36).slice(2, 9)
const titleId = `prompt-modal-title-${uid}`
const messageId = `prompt-modal-message-${uid}`
let focusTrap = null

function close() {
  emit('update:modelValue', false)
}

function onConfirm() {
  const v = inputValue.value
  emit('confirm', v)
  close()
}

function onCancel() {
  emit('cancel')
  close()
}

watch(() => props.modelValue, async (v) => {
  if (typeof document === 'undefined') return
  if (v) {
    // 初次挂载即为打开态也要填默认值、建立焦点陷阱并绑定 ESC。
    inputValue.value = props.defaultValue
    await nextTick()
    focusTrap = trapFocus(dialogRef.value, onCancel)
    inputRef.value?.focus?.()
    inputRef.value?.select?.()
  } else {
    focusTrap?.destroy()
    focusTrap = null
  }
}, { immediate: true })

onBeforeUnmount(() => {
  focusTrap?.destroy()
  focusTrap = null
})
</script>

<style scoped>
/* 其余 .modal-* 规则已收编至 styles/pages.css 的对话框家族 B（2026-10-30）。
   本组件消息下间距比 ConfirmModal 更紧（输入框紧随其后），故模板挂 .modal-message--tight。
   .modal-input 是本组件专属输入框，保留。 */
.modal-input {
  width: 100%;
  margin-bottom: 16px;
  padding: 8px 10px;
}
</style>