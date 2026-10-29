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

<!-- 样式已全部收编至 styles/pages.css 的对话框家族 B（.modal-overlay/.modal-box/
     .modal-title/.modal-message/.modal-actions 与 modal-enter/leave-*，2026-10-30）。
     本组件不再自带 scoped 样式。 -->