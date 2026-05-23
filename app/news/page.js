'use client';
import { useTranslation } from 'react-i18next';
import '../../lib/i18n';

export default function NewsPage() {
  const { t } = useTranslation();

  return (
    <div className="p-4">
      <h2 className="text-lg font-bold mb-4">{t('common.news')}</h2>
      <div className="space-y-4">
        <div className="border-b pb-4">
          <h3 className="font-semibold text-green-800">ሪፖርት | አዳማ ላይ የተደረጉ ጨዋታዎች ያለ ግብ ተጠናቀዋል</h3>
          <p className="text-sm text-gray-500 mt-1">በሳምንቱ መዝጊያ መርሐግብር አዳማ ከተማን ከ ወልዋሎ ዓ/ዩ እንዲሁም ነገሌ አርሲን ከምድረ ገነት ሽረ ያገናኙ ጨዋታዎች ዜሮ ለዜሮ በሆነ ውጤት ተጠናቀዋል::</p>
        </div>
        <div className="border-b pb-4">
          <h3 className="font-semibold text-green-800">የዋልያዎቹ አሰልጣኝ ከጨዋታው በኋላ ምን አሉ?</h3>
          <p className="text-sm text-gray-500 mt-1">የኢትዮጵያ ብሔራዊ ቡድን አሰልጣኝ ከዛሬው ጨዋታ በኋላ የሰጡት አስተያየት።</p>
        </div>
      </div>
    </div>
  );
}
