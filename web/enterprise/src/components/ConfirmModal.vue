<template>
  <Teleport to="body">
    <transition name="modal">
      <div v-if="modelValue" class="modal-overlay" data-testid="confirm-modal" @click.self="onCancel">
        <div
          class="modal-box"
          role="dialog"
          aria-modal="true"
          :aria-labelledby="titleId"
          :aria-describedby="messageId"
        >
          <h3 :id="titleId" class="modal-title">{{ title }}</h3>
          <p :id="messageId" class="modal-message">{{ message }}</p>
          <div class="modal-actions">
            <button v-if="!info" class="outline" @click="onCancel" data-testid="confirm-modal-cancel">
              {{ cancelText || $t('common.cancel') }}
            </button>
            <button ref="confirmBtnRef" class="primary" @click="onConfirm" data-testid="confirm-modal-confirm">
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
import { ref, watch } from 'vue'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  title: { type: String, default: '' },
  message: { type: String, default: '' },
  confirmText: { type: String, default: '' },
  cancelText: { type: String, default: '' },
  info: { type: Boolean, default: false }
})

const emit = defineEmits(['update:modelValue', 'confirm', 'cancel'])

// 审计整改批次11：无障碍语义——标题/描述唯一 id + 打开聚焦确认按钮/关闭还原焦点。
// role=dialog / aria-modal 绑定在 .modal-box（见模板）。
const uid = Math.random().toString(36).slice(2, 9)
const titleId = `confirm-modal-title-${uid}`
const messageId = `confirm-modal-message-${uid}`
const confirmBtnRef = ref(null)
let lastActive = null

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

// 兼容 ESC 关闭（非 info 模式）+ 焦点管理
if (typeof window !== 'undefined') {
  watch(
    () => props.modelValue,
    (v) => {
      if (v) {
        if (!props.info) window.addEventListener('keydown', onEsc)
        lastActive = document.activeElement
        requestAnimationFrame(() => confirmBtnRef.value?.focus?.())
      } else {
        window.removeEventListener('keydown', onEsc)
        if (lastActive && typeof lastActive.focus === 'function') lastActive.focus()
        lastActive = null
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