import i18n from 'i18next';
import { initReactI18next } from 'react-i18next';
import commonAm from '../locales/am/common.json';

const resources = {
  am: {
    translation: commonAm
  }
};

i18n
  .use(initReactI18next)
  .init({
    resources,
    lng: 'am',
    fallbackLng: 'am',
    interpolation: {
      escapeValue: false
    }
  });

export default i18n;
