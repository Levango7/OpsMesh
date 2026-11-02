// AI 提示词模板 store（2026-11-02 补「后端有、前端无界面」缺口）
// 后端契约：internal/ai/gateway/handler.go:368-378
//   GET    /api/v1/ai/prompts      → {templates: [PromptTemplate]}
//   POST   /api/v1/ai/prompts      → PromptTemplate
//   GET    /api/v1/ai/prompts/{id} → PromptTemplate
//   PUT    /api/v1/ai/prompts/{id} → PromptTemplate
//   DELETE /api/v1/ai/prompts/{id} → 204
import { defineStore } from 'pinia'
import { ref } from 'vue'
import { t } from '@/i18n'
import { listPrompts, createPrompt, updatePrompt, deletePrompt } from '@/api/ai'

export const useAIPromptsStore = defineStore('ai-prompts', () => {
  const templates = ref([])
  const loading = ref(false)
  const error = ref('')

  async function fetchTemplates(category) {
    loading.value = true
    error.value = ''
    try {
      const resp = await listPrompts(category)
      templates.value = resp.templates || []
    } catch (e) {
      error.value = e.j?.error || t('prompts.listFailed')
    } finally {
      loading.value = false
    }
  }

  async function saveTemplate(form) {
    const body = { name: form.name, category: form.category, content: form.content }
    const resp = form.id
      ? await updatePrompt(form.id, body)
      : await createPrompt(body)
    await fetchTemplates()
    return resp
  }

  async function removeTemplate(id) {
    await deletePrompt(id)
    await fetchTemplates()
  }

  return { templates, loading, error, fetchTemplates, saveTemplate, removeTemplate }
})
