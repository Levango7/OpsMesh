<template>
  <Teleport to="body">
    <transition name="modal">
      <div v-if="modelValue" class="modal-overlay" data-testid="confirm-modal" @click.self="onCancel">
        <div class="modal-box">
          <h3 class="modal-title">{{ title }}</h3>
          <p class="modal-message">{{ message }}</p>
          <div class="modal-actions">
            <button v-if="!info" class="outline" @click="onCancel" data-testid="confirm-modal-cancel">
              {{ cancelText || $t('common.cancel') }}
            </button>
            <button class="primary" @click="onConfirm" data-testid="confirm-modal-confirm">
              {{ confirmText || (info ? $t('common.ok') : $t('common.confirm')) }}
            </button>
          </div>
        </div>
      </div>
    </transition>
  </Teleport>
</template>

<script setup>
// 通用确认/信息对话框 — 替换原生 confirm()/alert()
// - 普通模式：显示「取消 + 确认」两个按钮，emit confirm/cancel
// - info 模式：只显示「确定」按钮，用于信息提示（替代 alert）
import { watch } from 'vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: '' },
  message: { type: String, default: '' },
  confirmText: { type: String, default: '' },
  cancelText: { type: String, default: '' },
  info: { type: Boolean, default: false }
})

const emit = defineEmits(['update:modelValue', 'confirm', 'cancel'])

function close() {
  emit('update:modelValue', false)
}

function onConfirm() {
  emit('confirm')
  close()
}

function onCancel() {
  // info 模式只有「确定」按钮，点击遮罩不关闭
  if (props.info) return
  emit('cancel')
  close()
}

// 兼容 ESC 关闭（非 info 模式）
if (typeof window !== 'undefined') {
  watch(
    () => props.modelValue,
    (v) => {
      if (v && !props.info) {
        window.addEventListener('keydown', onEsc)
      } else {
        window.removeEventListener('keydown', onEsc)
      }
    }
  )
}

function onEsc(e) {
  if (e.key === 'Escape' && props.modelValue && !props.info) {
    onCancel()
  }
}
</script>

<style scoped>
.modal-overlay {
  position: fixed;
  inset: 0;
  background: var(--modal-mask);
  display: flex;
  align-items: center;
  justify-content: center;
  z-index: 9999;
  padding: 16px;
}
.modal-box {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: var(--radius);
  box-shadow: 0 12px 40px rgba(31, 37, 64, 0.22);
  padding: 22px 24px;
  width: 100%;
  max-width: 420px;
}
.modal-title {
  margin: 0 0 10px;
  font-size: 15px;
  font-weight: 600;
  color: var(--text);
}
.modal-message {
  margin: 0 0 18px;
  color: var(--text-2);
  font-size: 13.5px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
}
.modal-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.modal-enter-active,
.modal-leave-active {
  transition: opacity 0.18s ease;
}
.modal-enter-active .modal-box,
.modal-leave-active .modal-box {
  transition: transform 0.18s ease, opacity 0.18s ease;
}
.modal-enter-from,
.modal-leave-to {
  opacity: 0;
}
.modal-enter-from .modal-box,
.modal-leave-to .modal-box {
  transform: translateY(-8px) scale(0.98);
  opacity: 0;
}
</style>