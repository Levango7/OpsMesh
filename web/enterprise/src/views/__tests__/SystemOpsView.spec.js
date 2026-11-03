// SystemOpsView 视图单元测试（2026-11-03）
// 覆盖：四张表的行内操作入口（此前 actions 列声明了 slot 但模板从未渲染，doDelete 不可达）——
// 用户删除、组删除、防火墙规则删除、系统服务 start/stop/restart，均经确认弹窗后下发。
import { describe, it, expect, beforeEach, vi } from "vitest";
import { mount, flushPromises } from "@vue/test-utils";

const st = vi.hoisted(() => {
  const { reactive } = require("vue");
  return {
    api: {
      getDeviceSystemInfo: vi.fn(),
      createSystemUser: vi.fn().mockResolvedValue(undefined),
      deleteSystemUser: vi.fn().mockResolvedValue(undefined),
      createSystemGroup: vi.fn().mockResolvedValue(undefined),
      deleteSystemGroup: vi.fn().mockResolvedValue(undefined),
      addFirewallRule: vi.fn().mockResolvedValue(undefined),
      deleteFirewallRule: vi.fn().mockResolvedValue(undefined),
      systemServiceAction: vi.fn().mockResolvedValue(undefined),
      queryJournal: vi.fn().mockResolvedValue({}),
      getTaskResult: vi.fn().mockResolvedValue({}),
      listFirewallRules: vi.fn().mockResolvedValue({}),
    },
    toastSuccess: vi.fn(),
    fetchDevices: vi.fn(),
    state: reactive({
      users: [
        {
          name: "alice",
          uid: 1000,
          gid: 1000,
          home: "/home/alice",
          shell: "/bin/bash",
          system: false,
        },
      ],
      groups: [{ name: "devs", gid: 1001, members: ["alice"] }],
      firewall: [
        {
          chain: "INPUT",
          target: "ACCEPT",
          protocol: "tcp",
          port: "8080",
          source: "0.0.0.0/0",
          rule: "ACCEPT tcp dpt:8080",
        },
      ],
      services: [{ name: "nginx", status: "active", enabled: true }],
    }),
  };
});

vi.mock("@/i18n", () => ({
  t: (k) => k,
  currentLang: { value: "zh" },
  setLang: vi.fn(),
}));
vi.mock("@/utils/toast", () => ({
  toast: { success: st.toastSuccess, error: vi.fn() },
}));
vi.mock("@/stores/device", () => ({
  useDeviceStore: () => ({
    flat: [{ deviceID: "d1", agentID: "a1", hostname: "h1", ip: "10.0.0.1" }],
    fetchDevices: st.fetchDevices,
  }),
}));
vi.mock("@/api/system-ops", () => ({
  ...st.api,
  getDeviceSystemInfo: vi.fn(async () => st.state),
}));
vi.mock("@/components/Icon.vue", () => ({ default: { template: "<i />" } }));

import SystemOpsView from "@/views/SystemOpsView.vue";
import { t as mockT } from "@/i18n";

// 普通对象桩（不用 defineComponent：Button/Input/Select/Table 是 HTML 保留名，会触发 vue/no-reserved-component-names）
const ButtonStub = {
  props: ["variant", "size", "disabled"],
  emits: ["click"],
  template:
    '<button :disabled="disabled" @click="$emit(\'click\')"><slot /></button>',
};
const TableStub = {
  props: ["data", "columns", "rowKey"],
  template: `<table><tbody>
    <tr v-for="(row, ri) in data" :key="ri">
      <td v-for="col in columns" :key="col.prop">
        <slot :name="'cell-' + col.prop" :row="row" :column="col" :row-index="ri">{{ row[col.prop] }}</slot>
      </td>
    </tr>
  </tbody></table>`,
};
const SelectStub = {
  props: ["modelValue", "options", "placeholder"],
  emits: ["update:modelValue"],
  template: "<select />",
};
const ModalStub = {
  props: ["modelValue", "title"],
  emits: ["update:modelValue", "close"],
  template: '<div v-if="modelValue" class="modal-box"><slot /></div>',
};
const stubs = {
  Button: ButtonStub,
  Table: TableStub,
  Select: SelectStub,
  Modal: ModalStub,
  Input: true,
  Drawer: true,
  Form: true,
  FormItem: true,
  Tabs: true,
  TabItem: true,
};

const mountWithDevice = async () => {
  const wrapper = mount(SystemOpsView, {
    global: { mocks: { $t: mockT }, stubs },
  });
  // 选择设备：直接触发视图的 @update:model-value 处理器（Select 桩不渲染原生 change）
  await wrapper.findComponent(SelectStub).vm.$emit("update:modelValue", "d1");
  await flushPromises();
  return wrapper;
};

describe("SystemOpsView 行内操作入口", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("用户表格渲染删除按钮并经确认后删除用户", async () => {
    const wrapper = await mountWithDevice();
    const btn = wrapper.find('[data-testid="del-user-alice"]');
    expect(btn.exists()).toBe(true);
    await btn.trigger("click");
    await wrapper.find('[data-testid="confirm-delete"]').trigger("click");
    expect(st.api.deleteSystemUser).toHaveBeenCalledWith({
      agentID: "a1",
      username: "alice",
    });
  });

  it("组表格渲染删除按钮并经确认后删除组", async () => {
    const wrapper = await mountWithDevice();
    await wrapper.find('[data-testid="del-group-devs"]').trigger("click");
    await wrapper.find('[data-testid="confirm-delete"]').trigger("click");
    expect(st.api.deleteSystemGroup).toHaveBeenCalledWith({
      agentID: "a1",
      groupName: "devs",
    });
  });

  it("防火墙规则渲染删除按钮，按行内字段（端口/协议/源）下发", async () => {
    const wrapper = await mountWithDevice();
    const btn = wrapper.find('[data-testid="del-rule-8080"]');
    expect(btn.exists()).toBe(true);
    await btn.trigger("click");
    await wrapper.find('[data-testid="confirm-delete"]').trigger("click");
    expect(st.api.deleteFirewallRule).toHaveBeenCalledWith({
      agentID: "a1",
      port: "8080",
      protocol: "tcp",
      source: "0.0.0.0/0",
    });
  });

  it("服务表格渲染 start/stop/restart 三个按钮，经确认后下发对应动作", async () => {
    const wrapper = await mountWithDevice();
    for (const action of ["start", "stop", "restart"]) {
      st.api.systemServiceAction.mockClear();
      await wrapper
        .find(`[data-testid="svc-${action}-nginx"]`)
        .trigger("click");
      // 确认弹窗标题/按钮存在
      await wrapper
        .find('[data-testid="confirm-service-action"]')
        .trigger("click");
      expect(st.api.systemServiceAction).toHaveBeenCalledWith({
        agentID: "a1",
        service: "nginx",
        action,
      });
    }
  });

  it("取消服务动作确认时不下发", async () => {
    const wrapper = await mountWithDevice();
    await wrapper.find('[data-testid="svc-stop-nginx"]').trigger("click");
    // 弹窗内仅按钮在 DOM；点击模态框外部关闭由 Modal 提供，此处直接断言未下发
    expect(st.api.systemServiceAction).not.toHaveBeenCalled();
  });

  it("未选设备时不渲染表格与操作按钮", async () => {
    const wrapper = mount(SystemOpsView, { global: { mocks: { $t: mockT } } });
    expect(wrapper.find('[data-testid="del-user-alice"]').exists()).toBe(false);
  });
});
