<script setup>
import { ref } from 'vue';
import InstanceList from '@/components/InstanceList.vue';
import ClientInfo from '@/components/ClientInfo.vue';
import InstanceInfo from '@/components/InstanceInfo.vue';
import ModelList from '@/components/ModelList.vue';
import ModelDetail from '@/components/ModelDetail.vue';

const rightDrawerOpen = ref(false);
const currentView = ref('instance'); // 当前视图：instance 或 model
const modelDetailUuid = ref(''); // 模型详情UUID

const rowProp = ref({});
const sessionIdProp = ref('');

let currentTab = '';
const tabs = {
  'client': ClientInfo,
  'instance': InstanceInfo
};

function showInfo(row, sessionId, type) {
  /**
   * 已开时
   *  不同切换
   *  相同关闭
   * 未开时打开
   */
  if (rightDrawerOpen.value) {
    if (type !== currentTab || rowProp.value !== row) {
      rowProp.value = row;
      currentTab = type;
      sessionIdProp.value = sessionId;
    } else {
      rightDrawerOpen.value = false;
    }
  } else {
    rightDrawerOpen.value = true;
    rowProp.value = row;
    currentTab = type;
    sessionIdProp.value = sessionId;
  }
}

// 切换视图
function switchView(view) {
  currentView.value = view;
  rightDrawerOpen.value = false;
}

// 显示模型详情
function showModelDetail(uuid) {
  modelDetailUuid.value = uuid;
  currentView.value = 'model-detail';
}
</script>

<template>
  <q-layout view="hHh lpR fFf">
    <!-- 顶部工具栏 -->
    <q-header elevated class="bg-primary text-white">
      <q-toolbar>
        <q-toolbar-title>ThingUE 启动程序</q-toolbar-title>

        <q-tabs v-model="currentView" @update:model-value="switchView">
          <q-tab name="instance" label="实例管理" icon="dns" />
          <q-tab name="model" label="模型库" icon="inventory_2" />
        </q-tabs>
      </q-toolbar>
    </q-header>

    <q-drawer v-model="rightDrawerOpen" :width="350" side="right" behavior="desktop" elevated>
      <component :is="tabs[currentTab]" :row="rowProp" :sessionId="sessionIdProp" @close="rightDrawerOpen = false"></component>
    </q-drawer>

    <q-page-container>
      <!-- 实例管理视图 -->
      <InstanceList v-if="currentView === 'instance'" @someEvent="showInfo" />

      <!-- 模型库列表视图 -->
      <ModelList v-else-if="currentView === 'model'" @show-detail="showModelDetail" />

      <!-- 模型详情视图 -->
      <ModelDetail v-else-if="currentView === 'model-detail'" :uuid="modelDetailUuid" @back="currentView = 'model'" />
    </q-page-container>
  </q-layout>
</template>

<style scoped></style>
