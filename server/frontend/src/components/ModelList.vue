<template>
  <div class="model-list">
    <!-- 顶部操作栏 -->
    <div class="toolbar">
      <div class="toolbar-left">
        <q-input
          v-model="searchKeyword"
          placeholder="搜索模型名称"
          style="width: 300px"
          dense
          outlined
          clearable
        >
          <template v-slot:prepend>
            <q-icon name="search" />
          </template>
        </q-input>
      </div>
      <div class="toolbar-right">
        <q-btn
          color="primary"
          icon="cloud_upload"
          label="上传模型"
          @click="showUploadDialog = true"
        />
      </div>
    </div>

    <!-- 筛选标签 -->
    <div class="filter-tags">
      <q-chip
        v-for="tag in tags"
        :key="tag"
        :color="selectedTag === tag ? 'primary' : 'grey-4'"
        :text-color="selectedTag === tag ? 'white' : 'black'"
        clickable
        @click="handleTagClick(tag)"
        class="q-mr-sm"
      >
        {{ tag }}
      </q-chip>
    </div>

    <!-- 模型卡片列表 -->
    <div class="model-grid-container">
      <q-inner-loading :showing="loading">
        <q-spinner-gears size="50px" color="primary" />
      </q-inner-loading>

      <div v-if="!loading && filteredModels.length === 0" class="empty-state">
        <q-icon name="inventory_2" size="80px" color="grey-5" />
        <div class="text-h6 text-grey-6 q-mt-md">暂无模型</div>
      </div>

      <div v-else class="model-grid">
        <q-card
          v-for="model in filteredModels"
          :key="model.uuid"
          class="model-card"
          @click="handleCardClick(model.uuid)"
        >
          <q-img
            :src="model.thumbnail"
            :alt="model.name"
            ratio="1"
            class="model-thumbnail"
          >
            <template v-slot:error>
              <div class="absolute-full flex flex-center bg-grey-3">
                <q-icon name="broken_image" size="50px" color="grey-5" />
              </div>
            </template>
          </q-img>

          <q-card-section class="model-info">
            <div class="model-name ellipsis" :title="model.name">{{ model.name }}</div>
            <div class="model-date text-grey-6">上传时间：{{ formatDate(model.createdAt) }}</div>
          </q-card-section>
        </q-card>
      </div>
    </div>

    <!-- 分页 -->
    <div v-if="totalModels > 0" class="pagination">
      <q-pagination
        v-model="currentPage"
        :max="totalPages"
        :max-pages="7"
        direction-links
        boundary-links
        @update:model-value="loadModels"
      />
      <q-select
        v-model="pageSize"
        :options="pageSizeOptions"
        dense
        outlined
        style="width: 100px; margin-left: 20px"
        @update:model-value="handlePageSizeChange"
      />
      <span class="q-ml-md text-grey-7">共 {{ totalModels }} 条</span>
    </div>

    <!-- 上传对话框 -->
    <UploadDialog
      v-model="showUploadDialog"
      @success="handleUploadSuccess"
    />
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue';
import { getModelList, getThumbnailUrl } from '@/model';
import { useQuasar } from 'quasar';
import UploadDialog from './UploadDialog.vue';

const $q = useQuasar();
const emit = defineEmits(['show-detail']);

// 搜索和筛选
const searchKeyword = ref('');
const selectedTag = ref('全部');
const tags = ref(['全部', 'Blueprint', 'StaticMesh', 'Material', 'Texture']);

// 分页
const currentPage = ref(1);
const pageSize = ref(12);
const pageSizeOptions = ref([12, 24, 48, 96]);
const totalModels = ref(0);

// 模型数据
const models = ref([]);
const loading = ref(false);

// 上传对话框
const showUploadDialog = ref(false);

// 计算总页数
const totalPages = computed(() => {
  return Math.ceil(totalModels.value / pageSize.value);
});

// 加载模型列表
const loadModels = async () => {
  loading.value = true;
  try {
    const params = {
      page: currentPage.value,
      pageSize: pageSize.value,
    };

    if (selectedTag.value !== '全部') {
      params.type = selectedTag.value;
    }

    const response = await getModelList(params);
    console.log('[ModelList] API Response:', response);
    console.log('[ModelList] response.code:', response?.code, 'type:', typeof response?.code);

    // 使用宽松比较，兼容字符串和数字类型的 code
    if (response.code == 200) {
      console.log('[ModelList] response.data.list:', response.data?.list);
      models.value = response.data.list.map((m) => ({
        ...m,
        thumbnail: getThumbnailUrl(m.uuid, '256x256', m.version),
      }));
      totalModels.value = response.data.total;
      console.log('[ModelList] models.value after assignment:', models.value);
    } else {
      console.log('[ModelList] Code not 200, actual code:', response.code);
      $q.notify({
        type: 'negative',
        message: '加载模型列表失败: ' + response.msg,
        position: 'top'
      });
    }
  } catch (error) {
    console.error('[ModelList] Error:', error);
    $q.notify({
      type: 'negative',
      message: '加载模型列表失败: ' + error.message,
      position: 'top'
    });
  } finally {
    loading.value = false;
  }
};

// 过滤后的模型列表
const filteredModels = computed(() => {
  let result = models.value;

  // 按关键词搜索
  if (searchKeyword.value) {
    result = result.filter((m) =>
      m.name.toLowerCase().includes(searchKeyword.value.toLowerCase())
    );
  }

  return result;
});

// 处理标签点击
const handleTagClick = (tag) => {
  selectedTag.value = tag;
  currentPage.value = 1;
  loadModels();
};

// 处理卡片点击
const handleCardClick = (uuid) => {
  emit('show-detail', uuid);
};

// 处理页面大小变化
const handlePageSizeChange = () => {
  currentPage.value = 1;
  loadModels();
};

// 处理上传成功
const handleUploadSuccess = () => {
  currentPage.value = 1;
  loadModels();
};

// 格式化日期
const formatDate = (dateStr) => {
  if (!dateStr) return '';
  const date = new Date(dateStr);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, '0');
  const day = String(date.getDate()).padStart(2, '0');
  return `${year}.${month}.${day}`;
};

// 页面加载时获取数据
onMounted(() => {
  loadModels();
});
</script>

<style scoped>
.model-list {
  width: 100%;
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 20px;
}

.toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 20px;
  flex-shrink: 0;
}

.filter-tags {
  margin-bottom: 20px;
  flex-shrink: 0;
}

/* 模型网格容器 */
.model-grid-container {
  position: relative;
  flex: 1;
  overflow-y: auto;
  margin-bottom: 20px;
}

/* 模型网格 - 优化为 1:1 正方形布局（256x256） */
.model-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(256px, 1fr));
  gap: 20px;
  padding-right: 10px;
}

/* 自定义滚动条样式 */
.model-grid-container::-webkit-scrollbar {
  width: 8px;
}

.model-grid-container::-webkit-scrollbar-track {
  background: #f1f1f1;
  border-radius: 4px;
}

.model-grid-container::-webkit-scrollbar-thumb {
  background: #c1c1c1;
  border-radius: 4px;
}

.model-grid-container::-webkit-scrollbar-thumb:hover {
  background: #a8a8a8;
}

.model-card {
  cursor: pointer;
  transition: all 0.3s ease;
  width: 256px;
  border-radius: 4px;
  overflow: hidden;
}

.model-card:hover {
  transform: translateY(-5px);
  box-shadow: 0 4px 12px rgba(0, 0, 0, 0.15);
}

.model-thumbnail {
  background-color: #f5f5f5;
  width: 256px;
  height: 256px;
}

.model-info {
  padding: 12px 16px;
  background-color: #fff;
}

.model-name {
  font-size: 14px;
  font-weight: 500;
  color: #333;
  margin-bottom: 8px;
  line-height: 1.4;
}

.model-date {
  font-size: 12px;
  color: #999;
  line-height: 1.4;
}

.pagination {
  display: flex;
  justify-content: center;
  align-items: center;
  margin-top: 20px;
  padding: 10px 0;
  flex-shrink: 0;
}

/* 空状态样式 */
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  height: 100%;
  min-height: 300px;
}

/* 响应式适配 - 针对不同的 PC 屏幕尺寸 */
@media screen and (min-width: 1920px) {
  .model-grid {
    grid-template-columns: repeat(auto-fill, minmax(256px, 1fr));
    gap: 24px;
  }
}

@media screen and (max-width: 1366px) {
  .model-grid {
    grid-template-columns: repeat(auto-fill, minmax(256px, 1fr));
    gap: 16px;
  }
}
</style>
